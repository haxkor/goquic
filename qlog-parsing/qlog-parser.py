import argparse
import logging
import os
import re
from datetime import datetime as dt

import matplotlib.pyplot as plt
import pandas

from rtp import *

logging.basicConfig(level="INFO")
logger = logging.getLogger()

argument_parser = argparse.ArgumentParser()
argument_parser.add_argument("output_dir")
argument_parser.add_argument(
    "-mr", "--mostrecent", default=0, type=int, help="parse the Nnth most recent run"
)
argument_parser.add_argument("-debug", action="store_true")
args = argument_parser.parse_args()

if args.debug:
    logger.setLevel("DEBUG")

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


def get_specific_log(regex):
    matching_logs = [entry for entry in run_files if re.match(regex, entry.name)]
    assert len(matching_logs) == 1
    return matching_logs[0]


client_rtp = get_specific_log(r".*client.*rtplog")
server_rtp = get_specific_log(r".*server.*rtplog")

# rtp sequence number -> timestamp
client_dict = make_seq_to_timestamp(client_rtp)
server_dict = make_seq_to_timestamp(server_rtp)


seq_to_client_delay = make_ts_to_client_delay(client_dict, server_dict)
arrive_delta_df = pandas.DataFrame.from_dict(
    seq_to_client_delay, orient="index", columns=["ts", "delta_to_client"]
)

distances_frames_sent = make_delays(server_dict)
sent_df = pandas.DataFrame.from_dict(
    distances_frames_sent,
    orient="index",
    columns=["ts", "delay_to_sending_next_packet"],
)


def pandas_plot(df):
    df.plot("ts")
    plt.show()


pandas_plot(arrive_delta_df)
