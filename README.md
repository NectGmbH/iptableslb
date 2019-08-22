# iptableslb

iptableslb is a tcp loadbalancer which uses iptables for the heavylifting.

## Installation

Setup a jump between your FORWARD chain and the iptableslb managed one:

`iptables -t filter -A FORWARD -j iptableslb-forward`

and also one for the NAT prerouting:

`iptables -t nat -A PREROUTING -j iptableslb-prerouting`

and if nat hairpinning is wanted, also postrouting:

`iptables -t nat -A PREROUTING -j iptableslb-hairpinning`

make sure those rules are appended after your firewall configs and before your "Drop everything else"-Rules