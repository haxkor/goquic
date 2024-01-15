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

    def bitrate_df(self):
        ts_to_bsent = dict()
        data_stream_id = 0  # todo

        for log_entry in self.server_log:
            key = int(log_entry["time"] / 1000)  # milliseconds to seconds
            ts_to_bsent.setdefault(key, 0)

            if log_entry.get("name") == "transport:packet_sent" and (data := log_entry.get("data")):
                if frames := data.get("frames"):
                    bytes_sent = 0
                    for f in frames:
                        if f.get("stream_id") != data_stream_id:
                            if length := f.get("length"):
                                bytes_sent += length

                    if bytes_sent:
                        ts_to_bsent[key] += bytes_sent


        ts_to_rate = {k : (k,v*8) for k,v in ts_to_bsent.items()}
        bitrate_df = pandas.DataFrame.from_dict(
            ts_to_rate,
            orient="index",
            columns=["ts", "bitrate"],
        )

        return bitrate_df
