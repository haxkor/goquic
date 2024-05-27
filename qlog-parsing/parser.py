import argparse
import logging
import os
import re
from datetime import datetime as dt

import matplotlib.pyplot as plt
import pandas

from qlog import *
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

with os.scandir(args.output_dir) as it:
    for entry in it:
        if entry.is_file() and not entry.name.startswith("."):
            match = re.match(r"(\d{4}-\d{2}-\d{2}-\d{2}:\d{2}:\d{2})(?:.*)", entry.name)
            try:
                ts = match.group(1)
            except AttributeError:
                logger.error("file %s did not match", entry.name)
                exit(1)

            files_from_same_run = collected_per_name.setdefault(ts, set())
            files_from_same_run.add(entry)

logger.debug("args: %s", args.mostrecent)
sorted_run_files = sorted(collected_per_name.items(), reverse=True)
# logger.debug("sorted_run_files: %s", sorted_run_files)
logger.info("using files of run %s", sorted_run_files[args.mostrecent][0])


def get_specific_log(regex):
    ts, run_files = sorted_run_files[args.mostrecent]
    matching_logs = [entry for entry in run_files if re.match(regex, entry.name)]
    if len(matching_logs) != 1:
        ts, next_run_files = sorted_run_files[args.mostrecent + 1]
        logger.warning("couldnt find file, trying again with ts %s \nfor %s", ts, regex)
        matching_logs = [
            entry for entry in next_run_files if re.match(regex, entry.name)
        ]

    assert len(matching_logs) == 1, "matching: %s" % matching_logs

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
sent_delta_df = pandas.DataFrame.from_dict(
    distances_frames_sent,
    orient="index",
    columns=["ts", "delay_to_sending_next_packet"],
)

client_qlog = get_specific_log(r".*client.*qlog")
server_qlog = get_specific_log(r".*server.*qlog")
logger.debug("server_qlog: %s", server_qlog)
run = qlog_run(server_qlog, client_qlog)


logger.setLevel("WARNING")
# arrive_delta_df.plot("ts")
run.bitrate_df().plot("ts")

# run.cansendrequests().plot("ts")

# run.rtt_slopes().plot("ts")

run.rtt_slopes_scores().plot("ts")

run.growthRate().plot("ts")
run.allowedBytes().plot("ts")
run.rateStatus().plot("ts")


plt.show()
