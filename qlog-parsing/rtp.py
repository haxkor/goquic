import json
from datetime import datetime as dt


def make_seq_to_timestamp(entry):
    with open(entry.path) as f:
        result = dict()
        for line in f.readlines():
            line_dict = json.loads(line)
            result[line_dict["seq"]] = line_dict["ts"]
        return result


def ts_diff(ts1: int, ts2: int):
    """returns the difference in microseconds"""
    diff = dt.fromtimestamp(ts1 / 1000) - dt.fromtimestamp(ts2 / 1000)
    # logger.debug("diff: %s", diff)
    return int(diff.total_seconds()) * 1_000_000 + diff.microseconds


def make_ts_to_client_delay(client_dict, server_dict):
    """returns a dict with {seq_num : (server_timestamp, delay_to_client)}"""
    smallest_seq = min(client_dict.keys())
    seq_to_diff = dict()
    for i in range(smallest_seq, smallest_seq + len(client_dict)):
        seq_to_diff[i] = (
            dt.fromtimestamp(server_dict[i] / 1000),
            ts_diff(client_dict[i], server_dict[i]),
        )

    return seq_to_diff


def make_delays(server_dict):
    """returns dict { seq_num : (timestamp, delay_to_next_rtp_sent/received) }
    does not consider time for transmission, only time between creating/receiving another RTP packet
    """
    smallest_seq = min(server_dict.keys())
    seq_to_diff = dict()

    prev = server_dict[smallest_seq]
    for i in range(smallest_seq + 1, smallest_seq + len(server_dict)):
        ts = server_dict[i]
        seq_to_diff[i] = (
            dt.fromtimestamp(prev / 1000),
            ts_diff(ts, prev),
        )
        prev = ts

    return seq_to_diff
