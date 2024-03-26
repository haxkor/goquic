#!/usr/bin/python3

import re
import sys
import logging
logging.basicConfig()
logger = logging.getLogger()
logger.setLevel("DEBUG")

regex = r".*slope is (.*)\\nfor"

logger.debug("sys argv: %s", sys.argv)

results = list()

with open(sys.argv[1]) as f:
    for line in f.readlines():
        # if "slope" in line: print(line)
        if m := re.match(regex, line):
            try:
                results.append(float(m.group(1)))
            except ValueError:
                pass

