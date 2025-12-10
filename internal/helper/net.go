package helper

import (
	"net"

	"github.com/dghubble/ipnets"
)

// Subnets takes a CIDR notation string and a new prefix length,
func Subnets(cidr string, newPrefix int) ([]*net.IPNet, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	subnets, err := ipnets.SubnetInto(network, newPrefix)
	if err != nil {
		return nil, err
	}
	return subnets, nil
}
