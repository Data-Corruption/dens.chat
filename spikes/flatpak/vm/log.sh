#!/bin/sh
# Shows the spike's log as it grows; Ctrl+C stops it.
exec tail -n 30 -f ~/.var/app/chat.dens.Spike/data/spike.log
