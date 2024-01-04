import json
from datetime import datetime as dt

import argparse
import re
import os

import pandas
import matplotlib.pyplot as plt


import logging
logging.basicConfig(level="INFO")
logger = logging.getLogger()

argument_parser = argparse.ArgumentParser()
argument_parser.add_argument("output_dir")
argument_parser.add_argument("-mr", "--mostrecent", default=0, type=int, help="parse the Nnth most recent run")
argument_parser.add_argument("-debug", action="store_true")
args = argument_parser.parse_args()

if args.debug: logger.setLevel("DEBUG")

# group log files via their (identical) timestamp, get the most recent
collected_per_name = dict()

for entry in os.scandir(args.output_dir):
    if entry.is_file():
        match = re.match(r"(\d{4}-\d{2}-\d{2}-\d{2}:\d{2}:\d{2})(?:.*)", entry.name)
        ts = match.group(1)
        files_from_same_run = collected_per_name.setdefault(ts, set())
        files_from_same_run.add(entry)

logger.debug("args: %s", args.mostrecent)
ts, run_files = sorted(collected_per_name.items(), reverse=True)[args.mostrecent]
logger.info("using files of run %s", ts)

client_rtp = [entry for entry in run_files if re.match(r".*client.*rtplog", entry.name)][0]
server_rtp = [entry for entry in run_files if re.match(r".*server.*rtplog", entry.name)][0]

rtp_seq_to_ts = dict()

def make_seq_to_timestamp(entry):
    with open(entry.path) as f:
        result = dict()
        for line in f.readlines():
            line_dict = json.loads(line)
            result[line_dict["seq"]] = line_dict["ts"]
        return result

def ts_diff(ts1:int, ts2:int):
    """returns the difference in microseconds"""
    diff = dt.fromtimestamp(ts1/1000) - dt.fromtimestamp(ts2/1000)
    #logger.debug("diff: %s", diff)
    return int(diff.total_seconds()) * 1_000_000 + diff.microseconds

# rtp sequence number -> timestamp
client_dict = make_seq_to_timestamp(client_rtp)
server_dict = make_seq_to_timestamp(server_rtp)

def make_ts_to_client_delay():
    """returns a dict with {seq_num : (server_timestamp, delay_to_client)} """
    smallest_seq = min(client_dict.keys())
    seq_to_diff = dict()
    for i in range(smallest_seq, smallest_seq + len(client_dict)):
        seq_to_diff[i] = (
            dt.fromtimestamp(server_dict[i]/1000), 
            ts_diff(client_dict[i], server_dict[i])
        )
    
    return seq_to_diff

def make_delays(num_to_ts):
    smallest_seq = min(num_to_ts.keys())
    seq_to_diff = dict()

    prev = num_to_ts[smallest_seq]
    for i in range(smallest_seq+1, smallest_seq + len(client_dict)):
        ts = num_to_ts[i]
        seq_to_diff[i] = (
            dt.fromtimestamp(prev/1000), 
            ts_diff(ts ,prev),
        )
        prev = ts

    return seq_to_diff


 

seq_to_client_delay = make_ts_to_client_delay()
df = pandas.DataFrame.from_dict(seq_to_client_delay, orient="index", columns=["ts", "delta_to_client"])

distances_frames_sent = make_delays(server_dict)
sent_df = pandas.DataFrame.from_dict(distances_frames_sent, orient="index", columns=["ts", "delay_to_sending_next_packet"])

def pandas_plot(df):
    df.plot("ts")
    plt.show()

pandas_plot(sent_df)






