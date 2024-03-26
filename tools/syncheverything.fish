#!/opt/homebrew/bin/fish
rsync -a -v --exclude={"/vm/*", "/Video/*", "*.git/", "*.qlog", "*.rtplog"}  ~/Documents/ma_quic/ ruehl@net-05.cm.in.tum.de:/home_stud/ruehl/ma/

