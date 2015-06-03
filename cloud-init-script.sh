#!/bin/bash
yum install go
mkdir workspace

echo "GOPATH=~/workspace" >> ~/.bash_profile
echo "REDIS_URL=r1.gmdl8l.0001.apse1.cache.amazonaws.com:6379 >> ~/.bash_profile
echo "REDIS_URL_KEY=REDIS_URL" >> ~/.bash_profile
echo "TOKEN=abcd1234" >> ~/.bash_profile
source ~/.bash_profile

go get github.com/runway7/satellite

echo "1024 65535" > /proc/sys/net/ipv4/ip_local_port_range
echo "*                -       nofile          999999" >>  /etc/security/limits.conf
echo "fs.file-max = 999999" >> /etc/sysctl.conf 
echo "net.ipv4.tcp_rmem = 4096 4096 16777216" >> /etc/sysctl.conf
echo "net.ipv4.tcp_wmem = 4096 4096 16777216" >> /etc/sysctl.conf
sysctl -p

workspace/bin/satellite


