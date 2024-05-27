import json
import os
import re
from logging import getLogger

import pandas

logger = getLogger()


def get_time_factor(script_location):
    REGEX = r"timefactor = (\d.?\d*)"
    with open(script_location) as f:
        match = re.search(REGEX, f.read())

    if not match:
        logger.warning("could not find timefactor, defaulting to 1")
        return 1

    return float(match.group(1))


def make_link_capacity():
    timefactor = get_time_factor(
        "/Users/jasperruhl/Documents/ma_quic/mininet-cc-tests/variable_capacity_single_flow/main.py"
    )
    data = [
        # duration, capacity
        (40 * timefactor, 1_000_000),
        (20 * timefactor, 2_500_000),
        (20 * timefactor, 600_000),
        (20 * timefactor, 1_000_000),
        (20 * timefactor, 3_000_000),
        (20 * timefactor + 4, 9_000_000),
    ]

    result = dict()
    key = 0
    for dur, cap in data:
        for _ in range(int(dur)):
            result[key] = cap
            key += 1
    return result


link_capacity = make_link_capacity()


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
            return result

        self.server_log = parse_single_log(self.server)
        self.client_log = parse_single_log(self.client)
        logger.debug("parsed qlogs! self.server_log=%s", self.server_log)

    def streams_transported(self):
        streamid_to_length = dict()

        for log_entry in self.server_log:
            if log_entry.get("name") != "transport:packet_sent":
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
                                # logger.debug("non4 stream_id %s", stream_id)

                    if bytes_sent:
                        ts_to_media_sent[key] += bytes_sent

        # generate a key : key, media, random, sum dict
        ts_to_rates = dict()
        for key in ts_to_media_sent.keys():
            media, random = ts_to_media_sent[key] * 8, ts_to_random_sent[key] * 8
            ts_to_rates[key] = (key, media, random, media + random, link_capacity[key])

        bitrate_df = pandas.DataFrame.from_dict(
            ts_to_rates,
            orient="index",
            columns=["ts", "media", "random", "sum", "cap"],
        )

        return bitrate_df

    def distance_between_bidiframes(self):
        timestamps = list()
        last = 0
        for log_entry in self.server_log:
            if log_entry.get("name") != "transport:UpdateLastBidiFrame":
                continue
            ts = log_entry.get("time") / 1000
            timestamps.append(ts - last)
            last = ts

        return timestamps

    def cansendrequests(self):
        ts_to_request_can = dict()
        ts_to_request_cannot = dict()

        for log_entry in self.server_log:
            ts = int(log_entry["time"] / 1000)
            ts_to_request_can.setdefault(ts, 0)
            ts_to_request_cannot.setdefault(ts, 0)

            if log_entry["name"] == "transport:CanSendUniFrame:":
                if log_entry["data"]["details"] == "can send uniframe":
                    ts_to_request_can[ts] += 1
                elif log_entry["data"]["details"] == "cant send uniframe":
                    ts_to_request_cannot[ts] += 1
                else:
                    raise NotImplementedError

        combined_dict = {
            ts: (ts, ts_to_request_can[ts], ts_to_request_cannot[ts])
            for ts in ts_to_request_can.keys()
        }

        df = pandas.DataFrame.from_dict(
            combined_dict,
            orient="index",
            columns=["ts", "can_send", "cannot_send"],
        )
        return df

    def rtt_slopes(self):
        to_short = dict()
        to_medium = dict()
        to_long = dict()

        for log_entry in self.server_log:
            name = log_entry["name"]
            if "transport:RTTRegress-" in name:
                # val = get_float(log_entry["data"])
                val = float(log_entry["data"]["details"])
                ts = float(log_entry["time"])

                if "Shortterm" in name:
                    to_short[ts] = val
                elif "Midterm" in name:
                    to_medium[ts] = val
                elif "Longterm" in name:
                    to_long[ts] = val
                else:
                    raise NotImplementedError

        assert len(to_short) == len(to_medium) == len(to_long)
        combined_dict = dict()
        first_ts = list(to_short.keys())[0]

        for short_ts, short_val, medium_val, long_val in zip(
            to_short.keys(), to_short.values(), to_medium.values(), to_long.values()
        ):
            ts = (short_ts - first_ts) / 1000
            combined_dict[ts] = (ts, short_val, medium_val, long_val)

        df = pandas.DataFrame.from_dict(
            combined_dict,
            orient="index",
            columns=["ts", "short", "medium", "long"],
        )
        return df

    def rtt_slopes_scores(self):
        to_short = dict()
        to_combined = dict()

        for log_entry in self.server_log:
            name = log_entry["name"]
            if "transport:RTTRegress_slopescore_" in name:
                # val = get_float(log_entry["data"])
                val = float(log_entry["data"]["details"])
                ts = float(log_entry["time"])

                if "short" in name:
                    to_short[ts] = val
                elif "combined" in name:
                    to_combined[ts] = val
                else:
                    raise NotImplementedError

        assert len(to_short) == len(to_combined)
        combined_dict = dict()
        first_ts = list(to_short.keys())[0]

        for short_ts, short_val, combined_val in zip(
            to_short.keys(), to_short.values(), to_combined.values()
        ):
            ts = (short_ts - first_ts) / 1000
            combined_dict[ts] = (ts, short_val, combined_val)

        df = pandas.DataFrame.from_dict(
            combined_dict,
            orient="index",
            columns=["ts", "short", "combined"],
        )
        return df

    def growthRate(self):

        to_growthrate = dict()

        for log_entry in self.server_log:
            name = log_entry["name"]
            if "transport:UpdateUnirate_growth" == name:
                ts = log_entry["time"] / 1000
                factor = float(log_entry["data"]["details"])
                to_growthrate[ts] = factor

        converted_dict = {k: (k, v) for k, v in to_growthrate.items()}

        df = pandas.DataFrame.from_dict(
            converted_dict, orient="index", columns=["ts", "growthRate"]
        )

        return df

    def allowedBytes(self):
        to_growthrate = dict()
        mostrecent_lastmax = 0

        for log_entry in self.server_log:
            name = log_entry["name"]

            if "transport:updated lastmax:" == name:
                mostrecent_lastmax = float(log_entry["data"]["details"])

            if "transport:UpdateUnirate_allowed_bytes" == name:
                ts = log_entry["time"] / 1000
                factor = float(log_entry["data"]["details"])
                to_growthrate[ts] = (factor, mostrecent_lastmax)

        converted_dict = {k: (k, f, l) for k, (f, l) in to_growthrate.items()}

        df = pandas.DataFrame.from_dict(
            converted_dict, orient="index", columns=["ts", "allowed_bytes", "lastmax"]
        )

        return df

    def rateStatus(self):
        to_ratestatus = dict()

        for log_entry in self.server_log:
            name = log_entry["name"]
            if "transport:UpdateUnirate-rateStatus" == name:
                ts = log_entry["time"] / 1000
                factor = float(log_entry["data"]["details"])
                to_ratestatus[ts] = factor

        converted_dict = {k: (k, v) for k, v in to_ratestatus.items()}

        df = pandas.DataFrame.from_dict(
            converted_dict, orient="index", columns=["ts", "ratestatus"]
        )

        return df


def avg(l):
    return sum(l) / len(l)
