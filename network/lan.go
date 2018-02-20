package network

import (
	"fmt"
	"net"
	"strings"
	"sync"
)

const LANDefaultttl = 10
const LANAliasesFile = "~/bettercap.aliases"

type EndpointNewCallback func(e *Endpoint)
type EndpointLostCallback func(e *Endpoint)

type LAN struct {
	sync.Mutex

	Hosts           map[string]*Endpoint `json:"hosts"`
	iface           *Endpoint
	gateway         *Endpoint
	ttl             map[string]uint
	aliases         *Aliases
	newCb           EndpointNewCallback
	lostCb          EndpointLostCallback
	aliasesFileName string
}

func NewLAN(iface, gateway *Endpoint, newcb EndpointNewCallback, lostcb EndpointLostCallback) *LAN {
	err, aliases := LoadAliases()
	if err != nil {
		fmt.Printf("%s\n", err)
	}

	return &LAN{
		iface:   iface,
		gateway: gateway,
		Hosts:   make(map[string]*Endpoint),
		ttl:     make(map[string]uint),
		aliases: aliases,
		newCb:   newcb,
		lostCb:  lostcb,
	}
}

func (lan *LAN) SetAliasFor(mac, alias string) bool {
	lan.Lock()
	defer lan.Unlock()

	mac = NormalizeMac(mac)
	if e, found := lan.Hosts[mac]; found {
		lan.aliases.Set(mac, alias)
		e.Alias = alias
		return true
	}
	return false
}

func (lan *LAN) Get(mac string) (*Endpoint, bool) {
	lan.Lock()
	defer lan.Unlock()

	if e, found := lan.Hosts[mac]; found == true {
		return e, true
	}
	return nil, false
}

func (lan *LAN) List() (list []*Endpoint) {
	lan.Lock()
	defer lan.Unlock()

	list = make([]*Endpoint, 0)
	for _, t := range lan.Hosts {
		list = append(list, t)
	}
	return
}

func (lan *LAN) WasMissed(mac string) bool {
	if mac == lan.iface.HwAddress || mac == lan.gateway.HwAddress {
		return false
	}

	lan.Lock()
	defer lan.Unlock()

	if ttl, found := lan.ttl[mac]; found == true {
		return ttl < LANDefaultttl
	}
	return true
}

func (lan *LAN) Remove(ip, mac string) {
	lan.Lock()
	defer lan.Unlock()

	if e, found := lan.Hosts[mac]; found {
		lan.ttl[mac]--
		if lan.ttl[mac] == 0 {
			delete(lan.Hosts, mac)
			delete(lan.ttl, mac)
			lan.lostCb(e)
		}
		return
	}
}

func (lan *LAN) shouldIgnore(ip, mac string) bool {
	// skip our own address
	if ip == lan.iface.IpAddress {
		return true
	}
	// skip the gateway
	if ip == lan.gateway.IpAddress {
		return true
	}
	// skip broadcast addresses
	if strings.HasSuffix(ip, BroadcastSuffix) {
		return true
	}
	// skip broadcast macs
	if strings.ToLower(mac) == BroadcastMac {
		return true
	}
	// skip everything which is not in our subnet (multicast noise)
	addr := net.ParseIP(ip)
	return lan.iface.Net.Contains(addr) == false
}

func (lan *LAN) Has(ip string) bool {
	lan.Lock()
	defer lan.Unlock()

	for _, e := range lan.Hosts {
		if e.IpAddress == ip {
			return true
		}
	}

	return false
}

func (lan *LAN) EachHost(cb func(mac string, e *Endpoint)) {
	lan.Lock()
	defer lan.Unlock()

	for m, h := range lan.Hosts {
		cb(m, h)
	}
}

func (lan *LAN) GetByIp(ip string) *Endpoint {
	lan.Lock()
	defer lan.Unlock()

	for _, e := range lan.Hosts {
		if e.IpAddress == ip {
			return e
		}
	}

	return nil
}

func (lan *LAN) AddIfNew(ip, mac string) *Endpoint {
	lan.Lock()
	defer lan.Unlock()

	mac = NormalizeMac(mac)

	if lan.shouldIgnore(ip, mac) {
		return nil
	} else if t, found := lan.Hosts[mac]; found {
		if lan.ttl[mac] < LANDefaultttl {
			lan.ttl[mac]++
		}
		return t
	}

	e := NewEndpointWithAlias(ip, mac, lan.aliases.Get(mac))

	lan.Hosts[mac] = e
	lan.ttl[mac] = LANDefaultttl

	lan.newCb(e)

	return nil
}