package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/quicvarint"

	"github.com/mengelbart/gst-go"
)

type pkt struct {
	flowID uint64
	length uint64
	buffer []byte
}

func read_pkt(stream quic.ReceiveStream) (pkt, error) {

	varintReader := quicvarint.NewReader(stream)
	length, err := quicvarint.Read(varintReader)

	buf := make([]byte, length)
	_, err = stream.Read(buf)
	if err != nil && err.Error() != "EOF" {
		panic(err)
	}
	return pkt{9, length, buf}, nil
}

func write_pkt(stream quic.SendStream, bytes []byte) (int, error) {
	// varintwriter := quicvarint.NewWriter(stream)
	initial_len := len(bytes)
	var payload []byte
	payload = quicvarint.Append(payload, 9)
	payload = quicvarint.Append(payload, uint64(initial_len))
	payload = append(payload, bytes...)

	_, err := stream.Write(payload)

	return initial_len, err

}

const addr = "localhost:4542"

const USE_ONE_STREAM bool = false
const USE_MANY_STREAMS bool = true
const USE_DATAGRAMS bool = false

func count_use_constants() int {
	constants := append(make([]bool, 0), USE_ONE_STREAM, USE_MANY_STREAMS, USE_DATAGRAMS)
	sum := 0
	for _, b := range constants {
		if b {
			sum += 1
		}
	}
	return sum
}

func main() {
	if count_use_constants() != 1 {
		panic("more than one USE_ constant is set")
	}
	fmt.Println("kek")
	isServer := flag.Bool("server", false, "server")
	flag.Parse()

	gst.GstInit()
	defer gst.GstDeinit()

	if *isServer {
		if err := server(); err != nil {
			log.Fatal(err)
		}
	} else {
		var client_func func() error
		if USE_MANY_STREAMS {
			client_func = client_many_streams
		} else if USE_ONE_STREAM {
			client_func = client
		} else if USE_DATAGRAMS {
			client_func = client_datagrams
		}

		if err := client_func(); err != nil {
			log.Fatal(err)
		}
	}
}
func server() error {
	gst_pipe, err := gst.NewPipeline("videotestsrc ! x264enc tune=zerolatency bitrate=500 speed-preset=superfast ! rtph264pay mtu=1300 ! appsink name=appsink")
	if err != nil {
		panic(err)
	}
	conf := &quic.Config{
		MaxIncomingStreams: 1 << 60,
		MaxIdleTimeout:     99999 * time.Second,
	}
	listener, err := quic.ListenAddr(addr, generateTLSConfig(), conf)
	if err != nil {
		return err
	}
	conn, err := listener.Accept(context.Background())
	if err != nil {
		return err
	}

	if USE_ONE_STREAM {
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			return err
		}

		gst_pipe.SetBufferHandler(func(buf gst.Buffer) {
			n, err := stream.Write(buf.Bytes)
			if err != nil {
				panic(err)
			}
			fmt.Printf("wrote %d bytes to buffer", n)
		})
		gst_pipe.Start()
	} else if USE_MANY_STREAMS {
		var streams_count int = 0
		gst_pipe.SetBufferHandler(func(buf gst.Buffer) {
			stream, err := conn.AcceptStream(context.Background())
			conn.SendMessage(buf.Bytes)
			streams_count += 1
			if err != nil {
				panic(err)
			}
			//n, err := stream.Write(buf.Bytes)
			n, err := write_pkt(stream, buf.Bytes)
			log.Printf("server wrote %d bytes to new stream, count = %d", n, streams_count)
			if err != nil {
				panic(err)
			}
			stream.Close()
		})
		gst_pipe.Start()

		time.Sleep(4 * time.Second)
		stream, err := conn.AcceptStream(context.Background())

		if err != nil {
			panic(err)
		}

		var payload []byte
		payload = quicvarint.Append(payload, 10)
		stream.Write(payload)

		r_desc, err := os.Open("trolol")
		if err != nil {
			panic(err)
		}
		defer r_desc.Close()

		io.Copy(stream, r_desc)
		stream.Close()

	} else if USE_DATAGRAMS {
		gst_pipe.SetBufferHandler(func(buf gst.Buffer) {
			if len(buf.Bytes) > 1300 {
				panic("gstreamer package too big")
			}
			log.Printf("sending message of length %v", len(buf.Bytes))
			conn.SendMessage(buf.Bytes)

		})
		gst_pipe.Start()
	}

	return nil
}

func client_datagrams() error {
	gst_pipe, err := gst.NewPipeline("appsrc ! \"application/x-rtp, media=(string)video, clock-rate=(int)90000, encoding-name=(string)H264, payload=(int)96\" ! rtpjitterbuffer ! rtph264depay ! decodebin ! videoconvert ! autovideosink sync=false ")

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-echo-example"},
	}
	conf := &quic.Config{
		MaxIdleTimeout:  99999 * time.Second,
		EnableDatagrams: true,
	}
	conn, err := quic.DialAddr(context.Background(), addr, tlsConf, conf)
	if err != nil {
		return err
	}
	go func() {
		for {
			log.Println("reading datagram")
			buf, err := conn.ReceiveMessage(context.TODO())
			if err != nil {
				log.Printf("error on receiving message: %v", err)
				gst_pipe.SendEOS()
			}
			log.Printf("received a datagram of length %v", len(buf))

			n, err := gst_pipe.Write(buf)
			log.Printf("wrote %v bytes ", n)
			if err != nil {
				log.Printf("error on write: %v", err)
				gst_pipe.SendEOS()
			}
		}
	}()

	gst_pipe.Start()
	return nil
}

func receive_big_file(stream quic.Stream) error {
	fmt.Println("receiving big file")
	w_desc, err := os.Create("trolol.copy")
	if err != nil {
		return err
	}
	defer w_desc.Close()

	_, err = io.Copy(w_desc, stream)
	stream.Close()

	panic("trolol written")
	return err
}

func client_many_streams() error {
	gst_pipe, err := gst.NewPipeline("appsrc ! \"application/x-rtp, media=(string)video, clock-rate=(int)90000, encoding-name=(string)H264, payload=(int)96\" ! rtpjitterbuffer ! rtph264depay ! decodebin ! videoconvert ! autovideosink sync=false ")

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-echo-example"},
	}
	conf := &quic.Config{
		MaxIdleTimeout: 99999 * time.Second,
	}
	conn, err := quic.DialAddr(context.Background(), addr, tlsConf, conf)
	if err != nil {
		return err
	}

	go func() {
		for {
			stream, err := conn.OpenStreamSync(context.Background())
			stream.Write([]byte("init hello"))
			fmt.Print("client opened stream")
			if err != nil {
				panic(err)
			}
			log.Println("reading from track")

			varintReader := quicvarint.NewReader(stream)
			id, err := quicvarint.Read(varintReader)
			log.Printf("read pkt id: %d", id)

			if id == 9 {
				pkt, err := read_pkt(stream)
				buf := pkt.buffer
				n := len(buf)

				if err != nil && err.Error() != "EOF" {
					log.Printf("error on read: %v", err)
					gst_pipe.SendEOS()
				}
				err = stream.Close()
				if err != nil {
					panic(err)
				}

				log.Printf("writing %v bytes from stream to pipeline", n)
				_, err = gst_pipe.Write(buf[:n])
				if err != nil {
					log.Printf("error on write: %v", err)
					gst_pipe.SendEOS()
				}

			} else if id == 10 {
				go receive_big_file(stream)
			} else {
				panic("got unknown id")
			}
		}
	}()

	ml := gst.NewMainLoop()
	ml.Run()

	return nil
}

func client() error {
	gst_pipe, err := gst.NewPipeline("appsrc ! \"application/x-rtp, media=(string)video, clock-rate=(int)90000, encoding-name=(string)H264, payload=(int)96\" ! rtpjitterbuffer ! rtph264depay ! decodebin ! videoconvert ! autovideosink sync=false ")

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-echo-example"},
	}
	conn, err := quic.DialAddr(context.Background(), addr, tlsConf, nil)
	if err != nil {
		return err
	}
	stream, err := conn.OpenStreamSync(context.Background())
	stream.Write([]byte("init hello"))
	fmt.Print("client opened stream")
	if err != nil {
		return err
	}

	go func() {
		for {
			log.Println("reading from track")
			buf := make([]byte, 64_000)
			n, err := stream.Read(buf)
			log.Printf("read %d lines", n)
			if err != nil {
				log.Printf("error on read: %v", err)
				gst_pipe.SendEOS()
			}
			log.Printf("writing %v bytes from stream to pipeline", n)
			_, err = gst_pipe.Write(buf[:n])
			if err != nil {
				log.Printf("error on write: %v", err)
				gst_pipe.SendEOS()
			}
		}
	}()

	ml := gst.NewMainLoop()
	ml.Run()

	return nil
}

// Setup a bare-bones TLS config for the server
func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"quic-echo-example"},
	}
}
