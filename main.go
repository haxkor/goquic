package main

import (
	"bufio"
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

	//	"math/rand"
	"os"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/logging"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/quic-go/quic-go/streamtypebalancer"

	"github.com/mengelbart/gst-go"
	"github.com/mengelbart/pace"

	"github.com/pion/rtp"
)

// mtu=1300
const client_pipe = "appsrc name=src ! application/x-rtp, media=(string)video, clock-rate=(int)90000, encoding-name=(string)H264, payload=(int)96 ! rtpjitterbuffer ! rtph264depay ! decodebin ! videoconvert ! autovideosink sync=false "

// identity
// const client_pipe = "appsrc name=src ! application/x-rtp, media=(string)video, clock-rate=(int)90000, encoding-name=(string)H264, payload=(int)96 ! identity dump=true ! rtpjitterbuffer ! rtph264depay ! decodebin ! videoconvert ! autovideosink sync=false "

const server_pipe = "videotestsrc ! x264enc tune=zerolatency bitrate=500 speed-preset=superfast ! rtph264pay  seqnum-offset=100 ! appsink name=appsink"

const addr = "localhost:4542"

const USE_ONE_STREAM bool = false
const USE_MANY_STREAMS bool = true
const USE_DATAGRAMS bool = false

const USE_BALANCER bool = false

type pkt struct {
	flowID uint64
	length uint64
	buffer []byte
}

// class that accumulates the bytes and logs once the parse method doesnt fail, then clears buffer
type RTP_logger struct {
	accumulator     []byte
	log_file_writer *bufio.Writer
}

func new_RTP_logger(logname string) *RTP_logger {
	f, err := os.Create(log_output_path + logname + ".rtplog")
	if err != nil {
		panic(err)
	}
	writer := bufio.NewWriter(f)

	result := RTP_logger{make([]byte, 0), writer} //why is this not a pointer?
	return &result
}

func (logger *RTP_logger) log_part(buf []byte) error {
	logger.accumulator = append(logger.accumulator, buf...)
	log.Printf("lenght of acc: %d", len(logger.accumulator))

	packet := rtp.Packet{}
	err := packet.Unmarshal(buf)
	if err != nil {
		log.Printf("not a full packet yet")
		log.Printf(err.Error())
		return nil
		panic(err)
	} else {
		logger.accumulator = make([]byte, 0) // seq nrs were also parsed without this line
	}
	log.Printf("seq nr: %d len: %d", packet.SequenceNumber, len(packet.Payload))
	log.Printf("lenght of acc: %d", len(logger.accumulator))
	logger.log_file_writer.Flush()
	_, err = fmt.Fprintf(logger.log_file_writer, "{\"seq\": %d, \"ts\": %d}\n", packet.SequenceNumber, time.Now().UnixMilli())
	if err != nil {
		panic(err)
	}
	return nil
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

var log_output_path string
var server_ip = flag.String("server_ip", "localhost", "at what address is the server created")

func main() {
	if count_use_constants() != 1 {
		panic("more than one USE_ constant is set")
	}
	fmt.Println("kek")

	isServer := flag.Bool("server", false, "server")
	output_path_arg := flag.String("output", "", "where to save the qlog")
	flag.Parse()

	log_output_path = *output_path_arg + "/" + time.Now().Format("2006-01-02-15:04:05")

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
	gst_pipe, err := gst.NewPipeline(server_pipe)
	if err != nil {
		panic(err)
	}
	conf := &quic.Config{
		MaxIncomingStreams: 1 << 60,
		MaxIdleTimeout:     99999 * time.Second,
	}

	if USE_BALANCER {
	conf.Tracer = func(ctx context.Context, p logging.Perspective, connID quic.ConnectionID) *logging.ConnectionTracer {
			return streamtypebalancer.MyNewTracer()
		}
	} else {
		conf.Tracer = qlog.DefaultTracer
	}
	// conf.Tracer = func(ctx context.Context, p logging.Perspective, connID quic.ConnectionID) *logging.ConnectionTracer {
	// 	if USE_BALANCER {
	// 		return streamtypebalancer.MyNewTracer()
	// 	} else {
	// 		filename := fmt.Sprintf("%sserver.qlog", log_output_path)
	// 		f, err := os.Create(filename)
	// 		if err != nil {
	// 			log.Fatal(err)
	// 		}
	// 		log.Printf("Creating qlog file %s.\n", filename)
	// 		return qlog.NewConnectionTracer(NewBufferedWriteCloser(bufio.NewWriter(f), f), p, connID)
	// 	}
	// }

	listener, err := quic.ListenAddr(*server_ip+":4242", generateTLSConfig(), conf)
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
		rtp_logger := new_RTP_logger("server_manystreams")
		gst_pipe.SetBufferHandler(func(buf gst.Buffer) {
			stream, err := conn.AcceptStream(context.Background())
			conn.SendDatagram(buf.Bytes)
			streams_count += 1
			if err != nil {
				panic(err)
			}
			n, err := write_pkt(stream, buf.Bytes)
			log.Printf("server wrote %d bytes to new stream, count = %d", n, streams_count)
			rtp_logger.log_part(buf.Bytes)

			log.Printf("still in MANY_STREAMS")
			if err != nil {
				panic(err)
			}
			stream.Close()
		})
		gst_pipe.Start()

		time.Sleep(4 * time.Second)
		// stream, err := conn.AcceptStream(context.Background())
		stream, err := conn.OpenUniStreamSync(context.Background())

		if err != nil {
			panic(err)
		}

		var payload []byte
		payload = quicvarint.Append(payload, 10)
		stream.Write(payload)

		limited_writer := pace.NewWriter(stream)
		limited_writer.SetRateLimit(1_000_000/2, 400)

		for {
			buf := make([]byte, 0x100)
			rand.Read(buf)
			_, err := limited_writer.Write(buf)
			if err != nil {
				panic(err)
			}
		}
		stream.Close()

	} else if USE_DATAGRAMS {
		gst_pipe.SetBufferHandler(func(buf gst.Buffer) {
			if len(buf.Bytes) > 1300 {
				panic("gstreamer package too big")
			}
			log.Printf("sending message of length %v", len(buf.Bytes))
			conn.SendDatagram(buf.Bytes)

		})
		gst_pipe.Start()
	}

	return nil
}

func client_datagrams() error {
	gst_pipe, err := gst.NewPipeline(client_pipe)

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-echo-example"},
	}
	conf := &quic.Config{
		MaxIdleTimeout:  99999 * time.Second,
		EnableDatagrams: true,
	}
	conf.Tracer = func(ctx context.Context, p logging.Perspective, connID quic.ConnectionID) *logging.ConnectionTracer {
		filename := fmt.Sprintf("%s/server.qlog", log_output_path)
		f, err := os.Create(filename)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Creating qlog file %s.\n", filename)
		return qlog.NewConnectionTracer(NewBufferedWriteCloser(bufio.NewWriter(f), f), p, connID)
	}

	conn, err := quic.DialAddr(context.Background(), *server_ip+":4242", tlsConf, conf)
	if err != nil {
		return err
	}
	go func() {
		for {
			log.Println("reading datagram")
			buf, err := conn.ReceiveDatagram(context.TODO())
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

func receive_random_bytes(stream quic.ReceiveStream) error {
	for {
		buf := make([]byte, 10000)
		n, err := stream.Read(buf)
		if err != nil {
			panic(err)
		}
		fmt.Printf("received %d randombytes\n", n)
	}
	return nil
}

func client_many_streams() error {
	gst_pipe, err := gst.NewPipeline(client_pipe)
	gst_pipe.Start()

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-echo-example"},
	}

	conf := &quic.Config{
		MaxIdleTimeout: 99999 * time.Second,
	}
	conf.Tracer = func(ctx context.Context, p logging.Perspective, connID quic.ConnectionID) *logging.ConnectionTracer {
		if USE_BALANCER {
			return streamtypebalancer.MyNewTracer()
		} else {
		filename := fmt.Sprintf("%sclient.qlog", log_output_path)
		f, err := os.Create(filename)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Creating qlog file %s.\n", filename)
			bufio.NewWriter(f)
		return qlog.NewConnectionTracer(NewBufferedWriteCloser(bufio.NewWriter(f), f), p, connID)
		}
	}

	conn, err := quic.DialAddr(context.Background(), *server_ip+":4242", tlsConf, conf)
	if err != nil {
		return err
	}

	rtp_logger := new_RTP_logger("client_manystreams")

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
				rtp_logger.log_part(buf)

				if err != nil && err.Error() != "EOF" {
					log.Printf("error on read: %v", err)
					gst_pipe.SendEOS()
				}
				err = stream.Close()
				if err != nil {
					panic(err)
				}

				log.Printf("writing %v bytes from stream to pipeline", n)
				n, err = gst_pipe.Write(buf[:n])
				if err != nil {
					log.Printf("error on write: %v", err)
					gst_pipe.SendEOS()
				}
				log.Printf("wrote %d bytes to the client pipeline", n)

			} else if id == 10 {
				go receive_random_bytes(stream)
			} else {
				panic("got unknown id")
			}
		}
	}()

	stream, err := conn.AcceptUniStream(context.Background())
	fmt.Print("client accepted Uni stream")
	if err != nil {
		panic(err)
	}
	log.Println("reading from track")
	go receive_random_bytes(stream)

	varintReader := quicvarint.NewReader(stream)
	id, err := quicvarint.Read(varintReader)
	log.Printf("read pkt id: %d", id)

	ml := gst.NewMainLoop()
	ml.Run()

	return nil
}

func client() error {
	gst_pipe, err := gst.NewPipeline(client_pipe)

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-echo-example"},
	}
	conn, err := quic.DialAddr(context.Background(), *server_ip+":4242", tlsConf, nil)
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

type bufferedWriteCloser struct {
	*bufio.Writer
	io.Closer
}

// NewBufferedWriteCloser creates an io.WriteCloser from a bufio.Writer and an io.Closer
func NewBufferedWriteCloser(writer *bufio.Writer, closer io.Closer) io.WriteCloser {
	return &bufferedWriteCloser{
		Writer: writer,
		Closer: closer,
	}
}

func (h bufferedWriteCloser) Close() error {
	if err := h.Writer.Flush(); err != nil {
		return err
	}
	return h.Closer.Close()
}
