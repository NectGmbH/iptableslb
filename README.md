# Installation

Setup a jump between your FORWARD chain and the iptableslb managed one:

`iptables -t filter -A FORWARD -j iptableslb-forward`

and also one for the NAT prerouting:

`iptables -t nat -A PREROUTING -j iptableslb-prerouting`

make sure those rules are after your firewall configs and before your "Drop everything else"-Rules