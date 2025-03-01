// Copyright 2019 Authors of Cilium
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package linux

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"

	"github.com/cilium/cilium/pkg/bpf"
	"github.com/cilium/cilium/pkg/byteorder"
	"github.com/cilium/cilium/pkg/datapath"
	"github.com/cilium/cilium/pkg/identity"
	"github.com/cilium/cilium/pkg/labels"
	bpfconfig "github.com/cilium/cilium/pkg/maps/configmap"
	"github.com/cilium/cilium/pkg/maps/ctmap"
	"github.com/cilium/cilium/pkg/maps/encrypt"
	"github.com/cilium/cilium/pkg/maps/eppolicymap"
	"github.com/cilium/cilium/pkg/maps/ipcache"
	ipcachemap "github.com/cilium/cilium/pkg/maps/ipcache"
	"github.com/cilium/cilium/pkg/maps/lbmap"
	"github.com/cilium/cilium/pkg/maps/lxcmap"
	"github.com/cilium/cilium/pkg/maps/metricsmap"
	"github.com/cilium/cilium/pkg/maps/nat"
	"github.com/cilium/cilium/pkg/maps/policymap"
	"github.com/cilium/cilium/pkg/maps/sockmap"
	"github.com/cilium/cilium/pkg/maps/tunnel"
	"github.com/cilium/cilium/pkg/node"
	"github.com/cilium/cilium/pkg/option"

	"github.com/vishvananda/netlink"
)

func writeIncludes(w io.Writer) (int, error) {
	return fmt.Fprintf(w, "#include \"lib/utils.h\"\n\n")
}

// WriteNodeConfig writes the local node configuration to the specified writer.
func (l *linuxDatapath) WriteNodeConfig(w io.Writer, cfg *datapath.LocalNodeConfiguration) error {
	extraMacrosMap := make(map[string]string)
	cDefinesMap := make(map[string]string)

	fw := bufio.NewWriter(w)

	writeIncludes(w)

	routerIP := node.GetIPv6Router()
	hostIP := node.GetIPv6()

	fmt.Fprintf(fw, ""+
		"/*\n"+
		" * Node-IPv6: %s\n"+
		" * Router-IPv6: %s\n"+
		" * Host-IPv4: %s\n"+
		" *\n"+
		" * External-IPv4: %s\n"+
		" * External-IPv6: %s\n"+
		" *\n"+
		" * NodePort-SNAT-IPv4: %s\n"+
		" * NodePort-SNAT-IPv6: %s\n"+
		" */\n\n",
		hostIP.String(), routerIP.String(),
		node.GetInternalIPv4().String(),
		node.GetExternalIPv4().String(),
		node.GetIPv6().String(),
		node.GetNodePortIPv4().String(),
		node.GetNodePortIPv6().String())

	if option.Config.EnableIPv6 {
		extraMacrosMap["ROUTER_IP"] = routerIP.String()
		fw.WriteString(defineIPv6("ROUTER_IP", routerIP))
		if option.Config.EnableNodePort {
			ipv6NP := node.GetNodePortIPv6()
			extraMacrosMap["IPV6_NODEPORT"] = ipv6NP.String()
			fw.WriteString(defineIPv6("IPV6_NODEPORT", ipv6NP))
		}
	}

	if option.Config.EnableIPv4 {
		ipv4GW := node.GetInternalIPv4()
		loopbackIPv4 := node.GetIPv4Loopback()
		ipv4Range := node.GetIPv4AllocRange()
		cDefinesMap["IPV4_GATEWAY"] = fmt.Sprintf("%#x", byteorder.HostSliceToNetwork(ipv4GW, reflect.Uint32).(uint32))
		cDefinesMap["IPV4_LOOPBACK"] = fmt.Sprintf("%#x", byteorder.HostSliceToNetwork(loopbackIPv4, reflect.Uint32).(uint32))
		cDefinesMap["IPV4_MASK"] = fmt.Sprintf("%#x", byteorder.HostSliceToNetwork(ipv4Range.Mask, reflect.Uint32).(uint32))

		if option.Config.EnableNodePort {
			ipv4NP := node.GetNodePortIPv4()
			cDefinesMap["IPV4_NODEPORT"] = fmt.Sprintf("%#x", byteorder.HostSliceToNetwork(ipv4NP, reflect.Uint32).(uint32))
		}
	}

	if nat46Range := option.Config.NAT46Prefix; nat46Range != nil {
		fw.WriteString(FmtDefineAddress("NAT46_PREFIX", nat46Range.IP))
	}

	extraMacrosMap["HOST_IP"] = hostIP.String()
	fw.WriteString(defineIPv6("HOST_IP", hostIP))

	cDefinesMap["HOST_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameHost))
	cDefinesMap["WORLD_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameWorld))
	cDefinesMap["HEALTH_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameHealth))
	cDefinesMap["UNMANAGED_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameUnmanaged))
	cDefinesMap["INIT_ID"] = fmt.Sprintf("%d", identity.GetReservedID(labels.IDNameInit))
	cDefinesMap["LB_RR_MAX_SEQ"] = fmt.Sprintf("%d", lbmap.MaxSeq)
	cDefinesMap["CILIUM_LB_MAP_MAX_ENTRIES"] = fmt.Sprintf("%d", lbmap.MaxEntries)
	cDefinesMap["TUNNEL_MAP"] = tunnel.MapName
	cDefinesMap["TUNNEL_ENDPOINT_MAP_SIZE"] = fmt.Sprintf("%d", tunnel.MaxEntries)
	cDefinesMap["ENDPOINTS_MAP"] = lxcmap.MapName
	cDefinesMap["ENDPOINTS_MAP_SIZE"] = fmt.Sprintf("%d", lxcmap.MaxEntries)
	cDefinesMap["METRICS_MAP"] = metricsmap.MapName
	cDefinesMap["METRICS_MAP_SIZE"] = fmt.Sprintf("%d", metricsmap.MaxEntries)
	cDefinesMap["POLICY_MAP_SIZE"] = fmt.Sprintf("%d", policymap.MaxEntries)
	cDefinesMap["IPCACHE_MAP"] = ipcachemap.Name
	cDefinesMap["IPCACHE_MAP_SIZE"] = fmt.Sprintf("%d", ipcachemap.MaxEntries)
	cDefinesMap["POLICY_PROG_MAP_SIZE"] = fmt.Sprintf("%d", policymap.ProgArrayMaxEntries)
	cDefinesMap["SOCKOPS_MAP_SIZE"] = fmt.Sprintf("%d", sockmap.MaxEntries)
	cDefinesMap["ENCRYPT_MAP"] = encrypt.MapName

	if option.Config.DatapathMode == option.DatapathModeIpvlan {
		cDefinesMap["ENABLE_SECCTX_FROM_IPCACHE"] = "1"
	}

	if option.Config.PreAllocateMaps {
		cDefinesMap["PREALLOCATE_MAPS"] = "1"
	}

	cDefinesMap["EVENTS_MAP"] = "cilium_events"
	cDefinesMap["POLICY_CALL_MAP"] = policymap.CallMapName
	cDefinesMap["EP_POLICY_MAP"] = eppolicymap.MapName
	cDefinesMap["LB6_REVERSE_NAT_MAP"] = "cilium_lb6_reverse_nat"
	cDefinesMap["LB6_SERVICES_MAP_V2"] = "cilium_lb6_services_v2"
	cDefinesMap["LB6_BACKEND_MAP"] = "cilium_lb6_backends"
	cDefinesMap["LB6_RR_SEQ_MAP_V2"] = "cilium_lb6_rr_seq_v2"
	cDefinesMap["LB6_REVERSE_NAT_SK_MAP"] = "cilium_lb6_reverse_sk"
	cDefinesMap["LB4_REVERSE_NAT_MAP"] = "cilium_lb4_reverse_nat"
	cDefinesMap["LB4_SERVICES_MAP_V2"] = "cilium_lb4_services_v2"
	cDefinesMap["LB4_RR_SEQ_MAP_V2"] = "cilium_lb4_rr_seq_v2"
	cDefinesMap["LB4_BACKEND_MAP"] = "cilium_lb4_backends"
	cDefinesMap["LB4_REVERSE_NAT_SK_MAP"] = "cilium_lb4_reverse_sk"

	cDefinesMap["TRACE_PAYLOAD_LEN"] = fmt.Sprintf("%dULL", option.Config.TracePayloadlen)
	cDefinesMap["MTU"] = fmt.Sprintf("%d", cfg.MtuConfig.GetDeviceMTU())

	if option.Config.EnableIPv4 {
		cDefinesMap["ENABLE_IPV4"] = "1"
	}

	if option.Config.EnableIPv6 {
		cDefinesMap["ENABLE_IPV6"] = "1"
	}

	if option.Config.EnableIPSec {
		cDefinesMap["ENABLE_IPSEC"] = "1"
	}

	if option.Config.EncryptNode {
		cDefinesMap["ENCRYPT_NODE"] = "1"
	}

	if option.Config.EnableHostReachableServices {
		if option.Config.EnableHostServicesTCP {
			cDefinesMap["ENABLE_HOST_SERVICES_TCP"] = "1"
		}
		if option.Config.EnableHostServicesUDP {
			cDefinesMap["ENABLE_HOST_SERVICES_UDP"] = "1"
		}
	}

	if option.Config.EncryptInterface != "" {
		link, err := netlink.LinkByName(option.Config.EncryptInterface)
		if err == nil {
			cDefinesMap["ENCRYPT_IFACE"] = fmt.Sprintf("%d", link.Attrs().Index)

			addr, err := netlink.AddrList(link, netlink.FAMILY_V4)
			if err == nil {
				a := byteorder.HostSliceToNetwork(addr[0].IPNet.IP, reflect.Uint32).(uint32)
				cDefinesMap["IPV4_ENCRYPT_IFACE"] = fmt.Sprintf("%d", a)
			}
		}
	}
	if option.Config.IsPodSubnetsDefined() {
		cDefinesMap["IP_POOLS"] = "1"
	}
	haveMasquerade := !option.Config.InstallIptRules && option.Config.Masquerade
	if haveMasquerade || option.Config.EnableNodePort {
		cDefinesMap["SNAT_COLLISION_RETRIES"] = fmt.Sprintf("%d", nat.CollisionRetriesDefault)
		cDefinesMap["SNAT_DETERMINISTIC_RETRIES"] = fmt.Sprintf("%d", nat.DeterministicRetriesDefault)

		if option.Config.EnableIPv4 {
			cDefinesMap["SNAT_MAPPING_IPV4"] = nat.MapNameSnat4Global
			cDefinesMap["SNAT_MAPPING_IPV4_SIZE"] = fmt.Sprintf("%d", option.Config.NATMapEntriesGlobal)
		}

		if option.Config.EnableIPv6 {
			cDefinesMap["SNAT_MAPPING_IPV6"] = nat.MapNameSnat6Global
			cDefinesMap["SNAT_MAPPING_IPV6_SIZE"] = fmt.Sprintf("%d", option.Config.NATMapEntriesGlobal)
		}
	}
	if haveMasquerade {
		cDefinesMap["ENABLE_MASQUERADE"] = "1"
		cDefinesMap["SNAT_MAPPING_MIN_PORT"] = fmt.Sprintf("%d", nat.MinPortSnatDefault)
		cDefinesMap["SNAT_MAPPING_MAX_PORT"] = fmt.Sprintf("%d", nat.MaxPortSnatDefault)

		// SNAT_DIRECTION is defined by init.sh
		if option.Config.EnableIPv4 {
			ipv4Addr := node.GetExternalIPv4()
			cDefinesMap["SNAT_IPV4_EXTERNAL"] = fmt.Sprintf("%#x", byteorder.HostSliceToNetwork(ipv4Addr, reflect.Uint32).(uint32))
		}

		if option.Config.EnableIPv6 {
			extraMacrosMap["SNAT_IPV6_EXTERNAL"] = hostIP.String()
			fw.WriteString(defineIPv6("SNAT_IPV6_EXTERNAL", hostIP))
		}
	}

	if (!option.Config.InstallIptRules && option.Config.Masquerade) || option.Config.EnableNodePort {
		ctmap.WriteBPFMacros(fw, nil)
	}

	if option.Config.EnableNodePort {
		cDefinesMap["ENABLE_NODEPORT"] = "1"
		cDefinesMap["NODEPORT_PORT_MIN"] = fmt.Sprintf("%d", option.Config.NodePortMin)
		cDefinesMap["NODEPORT_PORT_MAX"] = fmt.Sprintf("%d", option.Config.NodePortMax)
		cDefinesMap["NODEPORT_PORT_MIN_NAT"] = fmt.Sprintf("%d", option.Config.NodePortMax+1)
		cDefinesMap["NODEPORT_PORT_MAX_NAT"] = "43835"

		if option.Config.EnableIPv4 {
			cDefinesMap["NODEPORT_NEIGH4"] = "cilium_nodeport_neigh4"
		}
		if option.Config.EnableIPv6 {
			cDefinesMap["NODEPORT_NEIGH6"] = "cilium_nodeport_neigh6"
		}
	}

	// Since golang maps are unordered, we sort the keys in the map
	// to get a consistent writtern format to the writer. This maintains
	// the consistency when we try to calculate hash for a datapath after
	// writing the config.
	keys := []string{}
	for key := range cDefinesMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		fmt.Fprintf(fw, "#define %s %s\n", key, cDefinesMap[key])
	}

	// Populate cDefinesMap with extraMacrosMap to get all the configuration
	// in the cDefinesMap itself.
	for key, value := range extraMacrosMap {
		cDefinesMap[key] = value
	}

	// Write the JSON encoded config as base64 encoded commented string to
	// the header file.
	jsonBytes, err := json.Marshal(cDefinesMap)
	if err == nil {
		// We don't care if some error occurs while marshaling the map.
		// In such cases we skip embedding the base64 encoded JSON configuration
		// to the writer.
		encodedConfig := base64.StdEncoding.EncodeToString(jsonBytes)
		fmt.Fprintf(fw, "\n// JSON_OUTPUT: %s\n", encodedConfig)
	}

	return fw.Flush()
}

func (l *linuxDatapath) writeNetdevConfig(w io.Writer, cfg datapath.DeviceConfiguration) {
	fmt.Fprint(w, cfg.GetOptions().GetFmtList())
	if option.Config.IsFlannelMasterDeviceSet() {
		fmt.Fprint(w, "#define HOST_REDIRECT_TO_INGRESS 1\n")
	}

	// In case the Linux kernel doesn't support LPM map type, pass the set
	// of prefix length for the datapath to lookup the map.
	if ipcache.IPCache.MapType != bpf.BPF_MAP_TYPE_LPM_TRIE {
		ipcachePrefixes6, ipcachePrefixes4 := cfg.GetCIDRPrefixLengths()

		fmt.Fprint(w, "#define IPCACHE6_PREFIXES ")
		for _, prefix := range ipcachePrefixes6 {
			fmt.Fprintf(w, "%d,", prefix)
		}
		fmt.Fprint(w, "\n")
		fmt.Fprint(w, "#define IPCACHE4_PREFIXES ")
		for _, prefix := range ipcachePrefixes4 {
			fmt.Fprintf(w, "%d,", prefix)
		}
		fmt.Fprint(w, "\n")
	}
}

// WriteNetdevConfig writes the BPF configuration for the endpoint to a writer.
func (l *linuxDatapath) WriteNetdevConfig(w io.Writer, cfg datapath.DeviceConfiguration) error {
	fw := bufio.NewWriter(w)
	l.writeNetdevConfig(fw, cfg)
	return fw.Flush()
}

// writeStaticData writes the endpoint-specific static data defines to the
// specified writer. This must be kept in sync with loader.ELFSubstitutions().
func (l *linuxDatapath) writeStaticData(fw io.Writer, e datapath.EndpointConfiguration) {
	fmt.Fprint(fw, defineIPv6("LXC_IP", e.IPv6Address()))
	fmt.Fprint(fw, defineIPv4("LXC_IPV4", e.IPv4Address()))

	fmt.Fprint(fw, defineMAC("NODE_MAC", e.GetNodeMAC()))
	fmt.Fprint(fw, defineUint32("LXC_ID", uint32(e.GetID())))

	secID := e.GetIdentity().Uint32()
	fmt.Fprintf(fw, defineUint32("SECLABEL", secID))
	fmt.Fprintf(fw, defineUint32("SECLABEL_NB", byteorder.HostToNetwork(secID).(uint32)))

	epID := uint16(e.GetID())
	fmt.Fprintf(fw, "#define POLICY_MAP %s\n", bpf.LocalMapName(policymap.MapName, epID))
	fmt.Fprintf(fw, "#define CALLS_MAP %s\n", bpf.LocalMapName("cilium_calls_", epID))
	fmt.Fprintf(fw, "#define CONFIG_MAP %s\n", bpf.LocalMapName(bpfconfig.MapNamePrefix, epID))
}

// WriteEndpointConfig writes the BPF configuration for the endpoint to a writer.
func (l *linuxDatapath) WriteEndpointConfig(w io.Writer, e datapath.EndpointConfiguration) error {
	fw := bufio.NewWriter(w)

	writeIncludes(w)
	l.writeStaticData(fw, e)

	return l.writeTemplateConfig(fw, e)
}

func (l *linuxDatapath) writeTemplateConfig(fw *bufio.Writer, e datapath.EndpointConfiguration) error {
	if e.RequireEgressProg() {
		fmt.Fprintf(fw, "#define USE_BPF_PROG_FOR_INGRESS_POLICY 1\n")
	}

	if option.Config.ForceLocalPolicyEvalAtSource {
		fmt.Fprintf(fw, "#define FORCE_LOCAL_POLICY_EVAL_AT_SOURCE 1\n")
	}

	if e.RequireRouting() {
		fmt.Fprintf(fw, "#define ENABLE_ROUTING 1\n")
	}

	if !e.HasIpvlanDataPath() {
		if e.RequireARPPassthrough() {
			fmt.Fprint(fw, "#define ENABLE_ARP_PASSTHROUGH 1\n")
		} else {
			fmt.Fprint(fw, "#define ENABLE_ARP_RESPONDER 1\n")
		}

		fmt.Fprint(fw, "#define ENABLE_HOST_REDIRECT 1\n")
		if option.Config.IsFlannelMasterDeviceSet() {
			fmt.Fprint(fw, "#define HOST_REDIRECT_TO_INGRESS 1\n")
		}
	}

	if e.ConntrackLocalLocked() {
		ctmap.WriteBPFMacros(fw, e)
	} else {
		ctmap.WriteBPFMacros(fw, nil)
	}

	// Always enable L4 and L3 load balancer for now
	fmt.Fprint(fw, "#define LB_L3 1\n")
	fmt.Fprint(fw, "#define LB_L4 1\n")

	// Local delivery metrics should always be set for endpoint programs.
	fmt.Fprint(fw, "#define LOCAL_DELIVERY_METRICS 1\n")

	l.writeNetdevConfig(fw, e)

	return fw.Flush()
}

// WriteEndpointConfig writes the BPF configuration for the template to a writer.
func (l *linuxDatapath) WriteTemplateConfig(w io.Writer, e datapath.EndpointConfiguration) error {
	fw := bufio.NewWriter(w)
	return l.writeTemplateConfig(fw, e)
}
