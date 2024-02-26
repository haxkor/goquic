import json
import os
from logging import getLogger

import pandas

logger = getLogger()


def proxy_loads(line):
    logger.debug("line: %s", line)
    return json.loads(line)


class qlog_run:
    server = ""
    client = ""

    server_log = None
    client_log = None

    def __init__(self, server, client):
        self.server = server
        self.client = client

        self.parse_logs()

    def parse_logs(self):
        def parse_single_log(log_path):
            result = list()
            with open(log_path) as f:
                info = f.readline()
                for line in f.readlines():
                    try:
                        result.append(json.loads(line))
                    except json.decoder.JSONDecodeError:
                        return result

        self.server_log = parse_single_log(self.server)
        self.client_log = parse_single_log(self.client)

    def streams_transported(self):
        streamid_to_length = dict()

        for log_entry in self.server_log:
            if log_entry.get("name") != "transport:packet_sent" :
                continue

            if data := log_entry.get("data"):
                if frames := data.get("frames"):
                    for frame in frames:
                        if frame.get("length"):
                            stream_id = frame.get("stream_id")
                            streamid_to_length.setdefault(stream_id, 0)
                            streamid_to_length[stream_id] += frame.get("length")

        streamid_to_length.pop(None)
        return streamid_to_length




    def bitrate_df(self):
        ts_to_media_sent = dict()
        ts_to_random_sent = dict()
        data_stream_id = 0  # todo

        for log_entry in self.server_log:
            key = int(log_entry["time"] / 1000)  # milliseconds to seconds
            ts_to_media_sent.setdefault(key, 0)
            ts_to_random_sent.setdefault(key, 0)

            if log_entry.get("name") == "transport:packet_sent" and (
                data := log_entry.get("data")
            ):
                if frames := data.get("frames"):
                    bytes_sent = 0
                    for f in frames:
                        if stream_id := f.get("stream_id"):
                            if stream_id % 4 == 0:
                                if length := f.get("length"):
                                    ts_to_media_sent[key] += length
                            else:
                                if length := f.get("length"):
                                    ts_to_random_sent[key] += length
                                logger.debug("non4 stream_id %s", stream_id)

                    if bytes_sent:
                        ts_to_media_sent[key] += bytes_sent

        # generate a key : key, media, random, sum dict
        ts_to_rates = dict()
        for key in ts_to_media_sent.keys():
            media, random = ts_to_media_sent[key] * 8, ts_to_random_sent[key] * 8
            ts_to_rates[key] = (key, media, random, media + random)

        bitrate_df = pandas.DataFrame.from_dict(
            ts_to_rates,
            orient="index",
            columns=["ts", "media", "random", "sum"],
        )

        return bitrate_df


    def distance_between_bidiframes(self):
        timestamps = list()
        last = 0
        for log_entry in self.server_log:
            if log_entry.get("name") != "transport:UpdateLastBidiFrame":
                continue
            ts = log_entry.get("time") / 1000
            timestamps.append( ts -last)
            last = ts

        return timestamps



def avg(l):
    return sum(l) / len(l)



