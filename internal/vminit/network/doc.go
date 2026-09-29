// Package network gives the network card of the VM its address and its route, as a machine
// without a network manager has nobody else to do it. It speaks netlink to the kernel itself, with
// the constants of x/sys/unix: the three messages ip addr add, ip link set up and ip route add would
// send. It runs in the VM, on Linux only.
package network
