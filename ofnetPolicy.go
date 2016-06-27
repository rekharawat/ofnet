/***
Copyright 2014 Cisco Systems Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ofnet

import (
	"errors"
	"net"
	"net/rpc"
	"reflect"
	"strconv"

	log "github.com/Sirupsen/logrus"
	"github.com/contiv/ofnet/ofctrl"
)

// This file has security policy rule implementation

const TCP_FLAG_ACK = 0x10
const TCP_FLAG_SYN = 0x2

// PolicyRule has info about single rule
type PolicyRule struct {
	rule *OfnetPolicyRule // rule definition
	flow *ofctrl.Flow     // Flow associated with the flow
}

// PolicyAgent is an instance of a policy agent
type PolicyAgent struct {
	agent       *OfnetAgent             // Pointer back to ofnet agent that owns this
	ofSwitch    *ofctrl.OFSwitch        // openflow switch we are talking to
	dstGrpTable *ofctrl.Table           // dest group lookup table
	policyTable *ofctrl.Table           // Policy rule lookup table
	tepolicyTable *ofctrl.Table           // Policy rule lookup table - SRTE
	srmplsTable *ofctrl.Table           // Policy rule lookup table -SRTE
	nextTable   *ofctrl.Table           // Next table to goto for accepted packets
	Rules       map[string]*PolicyRule  // rules database
	DstGrpFlow  map[string]*ofctrl.Flow // FLow entries for dst group lookup
	SrMplsFlow  map[string]*ofctrl.Flow // FLow entries for srte srmpls 
	Policys     map[string]*OfnetPolicy  // policy database - SRTE
}

// NewPolicyMgr Creates a new policy manager
func NewPolicyAgent(agent *OfnetAgent, rpcServ *rpc.Server) *PolicyAgent {
	policyAgent := new(PolicyAgent)

	// initialize
	policyAgent.agent = agent
	policyAgent.Rules = make(map[string]*PolicyRule)
	policyAgent.Policys = make(map[string]*OfnetPolicy) //SRTE
	policyAgent.DstGrpFlow = make(map[string]*ofctrl.Flow)
	policyAgent.SrMplsFlow = make(map[string]*ofctrl.Flow) //SRTE

	// Register for Master add/remove events
	rpcServ.Register(policyAgent)

	// done
	return policyAgent
}

// Handle switch connected notification
func (self *PolicyAgent) SwitchConnected(sw *ofctrl.OFSwitch) {
	// Keep a reference to the switch
	self.ofSwitch = sw

	log.Infof("Switch connected(policyAgent).")
}

// Handle switch disconnected notification
func (self *PolicyAgent) SwitchDisconnected(sw *ofctrl.OFSwitch) {
	// FIXME: ??
}

// Metadata Format
//	 6			   3 3			   1 1			   0 0
//	 3			   1 0             6 5			   1 0
//  +-------------+-+---------------+---------------+-+
//  |	....U	  |U|    SrcGrp     |    DstGrp     |V|
//  +-------------+-+---------------+---------------+-+
//
//	U: Unused
//  SrcGrp:  Source endpoint group
//  DstGrp: Destination endpoint group
//  V: Received on VTEP Port. Dont flood back to VTEP ports.
//

// DstGroupMetadata returns metadata for dst group
func DstGroupMetadata(groupId int) (uint64, uint64) {
	metadata := uint64(groupId) << 1
	metadataMask := uint64(0xfffe)
	metadata = metadata & metadataMask

	return metadata, metadataMask
}

// SrcGroupMetadata returns metadata for src group
func SrcGroupMetadata(groupId int) (uint64, uint64) {
	metadata := uint64(groupId) << 16
	metadataMask := uint64(0x7fff0000)
	metadata = metadata & metadataMask

	return metadata, metadataMask
}

// ruleIsSame check if two rules are identical
func ruleIsSame(r1, r2 *OfnetPolicyRule) bool {
	return reflect.DeepEqual(*r1, *r2)
}

// AddEndpoint adds an endpoint to dst group lookup
func (self *PolicyAgent) AddEndpoint(endpoint *OfnetEndpoint) error {

	if self.DstGrpFlow[endpoint.EndpointID] != nil {
		// FIXME: handle this as Update
		log.Warnf("DstGroup for endpoint %+v already exists", endpoint)
		return nil
	}

	log.Infof("Adding dst group entry for endpoint: %+v", endpoint)
	vrf := self.agent.vlanVrf[endpoint.Vlan]

	log.Infof("Recevied add endpoint for vrf %v", *vrf)

	vrfid := self.agent.vrfNameIdMap[*vrf]
	vrfMetadata, vrfMetadataMask := Vrfmetadata(*vrfid)
	// Install the Dst group lookup flow
	dstGrpFlow, err := self.dstGrpTable.NewFlow(ofctrl.FlowMatch{
		Priority:     FLOW_MATCH_PRIORITY,
		Ethertype:    0x0800,
		IpDa:         &endpoint.IpAddr,
		Metadata:     &vrfMetadata,
		MetadataMask: &vrfMetadataMask,
	})
	if err != nil {
		log.Errorf("Error adding dstGroup flow for %v. Err: %v", endpoint.IpAddr, err)
		return err
	}

	// Format the metadata
	metadata, metadataMask := DstGroupMetadata(endpoint.EndpointGroup)

	// Set dst GroupId
	err = dstGrpFlow.SetMetadata(metadata, metadataMask)
	if err != nil {
		log.Errorf("Error setting metadata %v for flow {%+v}. Err: %v", metadata, dstGrpFlow, err)
		return err
	}

	// Go to policy Table
	err = dstGrpFlow.Next(self.policyTable)
	if err != nil {
		log.Errorf("Error installing flow {%+v}. Err: %v", dstGrpFlow, err)
		return err
	}

	// save the Flow
	self.DstGrpFlow[endpoint.EndpointID] = dstGrpFlow


	// Add flow for TE policies - SRTE
	/*`
	for _, policy := range self.Policys {
		log.Infof("Policy in self.Policys in endpt: %+v", policy)
	 	if(policy.EndptgpID == endpoint.EndpointGroup) && ( policy.PolicyType == "TEPolicy") {
	 		macAddr, err := net.ParseMAC(endpoint.MacAddrStr)
				if err != nil {
			 		return err
			}
	 		// Default flow in srmpls table - SRTE
			srmplsTable := self.ofSwitch.GetTable(SRMPLS_TBL_ID)

			srmplsFlow, err := srmplsTable.NewFlow(ofctrl.FlowMatch{
				Ethertype: 0x8847,
				Priority: FLOW_MISS_PRIORITY,
				MacSa:    &macAddr,
			})
			if err != nil {
				log.Errorf("Error creating srmpls flow for endpoint %+v. Err: %v", endpoint, err)
				return err
			}
			srmplsFlow.PopMpls()
			macDestTable := self.ofSwitch.GetTable(IP_TBL_ID)
			err = srmplsFlow.Next(macDestTable)
			if err != nil {
				log.Errorf("Error installing srmpls entry. Err: %v", err)
				return err
			}
			self.SrMplsFlow[endpoint.EndpointID] = srmplsFlow
	 	}
	 }
	*/
	return nil
}

// DelEndpoint deletes an endpoint from dst group lookup
func (self *PolicyAgent) DelEndpoint(endpoint *OfnetEndpoint) error {

	// Remove the port vlan flow.
	srMplsFlow := self.SrMplsFlow[endpoint.EndpointID]
	if srMplsFlow != nil {
		err := srMplsFlow.Delete()
		if err != nil {
			log.Errorf("Error deleting srMplsFlow flow. Err: %v", err)
		}
	}
	// find the dst group flow

	// find the dst group flow
	dstGrp := self.DstGrpFlow[endpoint.EndpointID]
	if dstGrp == nil {
		return errors.New("Dst Group not found")
	}

	// delete the Flow
	err := dstGrp.Delete()
	if err != nil {
		log.Errorf("Error deleting dst group for endpoint: %+v. Err: %v", endpoint, err)
	}

	// delete the cache
	delete(self.DstGrpFlow, endpoint.EndpointID)

	return nil
}

// AddIpv6Endpoint adds an endpoint to dst group lookup
func (self *PolicyAgent) AddIpv6Endpoint(endpoint *OfnetEndpoint) error {

	if endpoint.Ipv6Addr == nil {
		log.Warnf("DstGroup for IPv6 endpoint %+v without Ipv6Addr", endpoint)
		return nil
	}

	ipv6EpId := self.agent.getEndpointIdByIpVlan(endpoint.Ipv6Addr, endpoint.Vlan)
	if self.DstGrpFlow[ipv6EpId] != nil {
		// FIXME: handle this as Update
		log.Warnf("DstGroup for IPv6 endpoint %+v already exists", endpoint)
		return nil
	}

	log.Infof("Adding dst group entry for endpoint: %+v", endpoint)
	vrf := self.agent.vlanVrf[endpoint.Vlan]

	log.Infof("Recevied add endpoint for vrf %v", *vrf)

	vrfid := self.agent.vrfNameIdMap[*vrf]
	vrfMetadata, vrfMetadataMask := Vrfmetadata(*vrfid)
	// Install the Dst group lookup flow
	dstGrpFlow, err := self.dstGrpTable.NewFlow(ofctrl.FlowMatch{
		Priority:     FLOW_MATCH_PRIORITY,
		Ethertype:    0x86DD,
		Ipv6Da:       &endpoint.Ipv6Addr,
		Metadata:     &vrfMetadata,
		MetadataMask: &vrfMetadataMask,
	})
	if err != nil {
		log.Errorf("Error adding dstGroup flow for %v. Err: %v", endpoint.IpAddr, err)
		return err
	}

	// Format the metadata
	metadata, metadataMask := DstGroupMetadata(endpoint.EndpointGroup)

	// Set dst GroupId
	err = dstGrpFlow.SetMetadata(metadata, metadataMask)
	if err != nil {
		log.Errorf("Error setting metadata %v for flow {%+v}. Err: %v", metadata, dstGrpFlow, err)
		return err
	}

	// Go to policy Table
	err = dstGrpFlow.Next(self.policyTable)
	if err != nil {
		log.Errorf("Error installing flow {%+v}. Err: %v", dstGrpFlow, err)
		return err
	}

	// save the Flow
	self.DstGrpFlow[ipv6EpId] = dstGrpFlow

	return nil
}

// DelIpv6Endpoint deletes an endpoint from dst group lookup
func (self *PolicyAgent) DelIpv6Endpoint(endpoint *OfnetEndpoint) error {

	// find the dst group IPv6 flow
	ipv6EpId := self.agent.getEndpointIdByIpVlan(endpoint.Ipv6Addr, endpoint.Vlan)
	dstGrp := self.DstGrpFlow[ipv6EpId]
	if dstGrp == nil {
		return errors.New("Dst Group IPv6 Flow not found")
	}

	// delete the Flow
	err := dstGrp.Delete()
	if err != nil {
		log.Errorf("Error deleting dst group for IPv6 endpoint: %+v. Err: %v", endpoint, err)
	}

	// delete the cache
	delete(self.DstGrpFlow, ipv6EpId)

	return nil
}

// AddRule adds a security rule to policy table
func (self *PolicyAgent) AttachPolicy(ofnetPol *OfnetPolicy ,  ret *bool) error {
	
	self.Policys[ofnetPol.PolicyId] = ofnetPol

	return nil
}

// DetachPolicy deletes policy table
func (self *PolicyAgent) DetachPolicy(epgPolicyKey string , ret *bool) error {
	log.Infof("Received DetachPolicy: %+v", epgPolicyKey)

	// Delete the policy from cache
	delete(self.Policys, epgPolicyKey)

	return nil
}

// AddRule adds a security rule to policy table
func (self *PolicyAgent) AddRule(rule *OfnetPolicyRule, ret *bool) error {
	var ipDa *net.IP = nil
	var ipDaMask *net.IP = nil
	var ipSa *net.IP = nil
	var ipSaMask *net.IP = nil
	var md *uint64 = nil
	var mdm *uint64 = nil
	var flag, flagMask uint16
	var flagPtr, flagMaskPtr *uint16
	var err error

	// check if we already have the rule
	if self.Rules[rule.RuleId] != nil {
		oldRule := self.Rules[rule.RuleId].rule

		if ruleIsSame(oldRule, rule) {
			return nil
		} else {
			log.Errorf("Rule already exists. new rule: {%+v}, old rule: {%+v}", rule, oldRule)
			return errors.New("Rule already exists")
		}
	}

	log.Infof("Received AddRule: %+v", rule)

	// Parse dst ip
	if rule.DstIpAddr != "" {
		ipDa, ipDaMask, err = ParseIPAddrMaskString(rule.DstIpAddr)
		if err != nil {
			log.Errorf("Error parsing dst ip %s. Err: %v", rule.DstIpAddr, err)
			return err
		}
	}

	// parse src ip
	if rule.SrcIpAddr != "" {
		ipSa, ipSaMask, err = ParseIPAddrMaskString(rule.SrcIpAddr)
		if err != nil {
			log.Errorf("Error parsing src ip %s. Err: %v", rule.SrcIpAddr, err)
			return err
		}
	}

	// parse source/dst endpoint groups
	if rule.SrcEndpointGroup != 0 && rule.DstEndpointGroup != 0 {
		srcMetadata, srcMetadataMask := SrcGroupMetadata(rule.SrcEndpointGroup)
		dstMetadata, dstMetadataMask := DstGroupMetadata(rule.DstEndpointGroup)
		metadata := srcMetadata | dstMetadata
		metadataMask := srcMetadataMask | dstMetadataMask
		md = &metadata
		mdm = &metadataMask
	} else if rule.SrcEndpointGroup != 0 {
		srcMetadata, srcMetadataMask := SrcGroupMetadata(rule.SrcEndpointGroup)
		md = &srcMetadata
		mdm = &srcMetadataMask
	} else if rule.DstEndpointGroup != 0 {
		dstMetadata, dstMetadataMask := DstGroupMetadata(rule.DstEndpointGroup)
		md = &dstMetadata
		mdm = &dstMetadataMask
	}

	// Setup TCP flags
	if rule.IpProtocol == 6 && rule.TcpFlags != "" {
		switch rule.TcpFlags {
		case "syn":
			flag = TCP_FLAG_SYN
			flagMask = TCP_FLAG_SYN
		case "syn,ack":
			flag = TCP_FLAG_ACK | TCP_FLAG_SYN
			flagMask = TCP_FLAG_ACK | TCP_FLAG_SYN
		case "ack":
			flag = TCP_FLAG_ACK
			flagMask = TCP_FLAG_ACK
		case "syn,!ack":
			flag = TCP_FLAG_SYN
			flagMask = TCP_FLAG_ACK | TCP_FLAG_SYN
		case "!syn,ack":
			flag = TCP_FLAG_ACK
			flagMask = TCP_FLAG_ACK | TCP_FLAG_SYN
		default:
			log.Errorf("Unknown TCP flags: %s, in rule: %+v", rule.TcpFlags, rule)
			return errors.New("Unknown TCP flag")
		}

		flagPtr = &flag
		flagMaskPtr = &flagMask
	}
	if (( rule.Action == "allow" ) ||  ( rule.Action == "deny" )) { 
		// Install the rule in policy table
		ruleFlow, err := self.policyTable.NewFlow(ofctrl.FlowMatch{
			Priority:     uint16(FLOW_POLICY_PRIORITY_OFFSET + rule.Priority),
			Ethertype:    0x0800,
			IpDa:         ipDa,
			IpDaMask:     ipDaMask,
			IpSa:         ipSa,
			IpSaMask:     ipSaMask,
			IpProto:      rule.IpProtocol,
			TcpSrcPort:   rule.SrcPort,
			TcpDstPort:   rule.DstPort,
			UdpSrcPort:   rule.SrcPort,
			UdpDstPort:   rule.DstPort,
			Metadata:     md,
			MetadataMask: mdm,
			TcpFlags:     flagPtr,
			TcpFlagsMask: flagMaskPtr,
		})
		if err != nil {
			log.Errorf("Error adding flow for rule {%v}. Err: %v", rule, err)
			return err
		}
		if rule.Action == "allow" {
			err = ruleFlow.Next(self.nextTable)
			if err != nil {
				log.Errorf("Error installing flow {%+v}. Err: %v", ruleFlow, err)
				return err
			}
		} else if rule.Action == "deny" {
			err = ruleFlow.Next(self.ofSwitch.DropAction())
			if err != nil {
				log.Errorf("Error installing flow {%+v}. Err: %v", ruleFlow, err)
				return err
			}
		}
			// save the rule
		pRule := PolicyRule{
			rule: rule,
			flow: ruleFlow,
		}
		self.Rules[rule.RuleId] = &pRule
	} else if rule.Action == "sla" {  //SRTE Rule action

		log.Infof("SLA is : %+v", rule.Sla)
		polFlow1, err := self.policyTable.NewFlow(ofctrl.FlowMatch{
                        Priority: FLOW_MATCH_PRIORITY,
                        Ethertype:    0x0800,
                        Metadata:     md,
                        MetadataMask: mdm,
                })
                if err != nil {
                        log.Errorf("Error creating policy flow {%+v}. Err: %v", polFlow1, err)
                        return err
                }

                //polFlow1.PushMpls(rule.Sla)
		//sla1,_ := strconv.ParseUint(rule.Sla,0,64)
		sla1 :=  0x900000000000
		if rule.Sla == "Highest Bandwidth"{
			sla1 :=  0x900200000000
		} else if rule.Sla == "Lowest Latency"{
                        sla1 :=  0x900000000000
		} else if rule.Sla == "Secure Path"{
                        sla1 :=  0x900100000000
		}
		//polFlow1.SetMetadata(uint64(rule.Sla),uint64(rule.Sla))
		polFlow1.SetMetadata(sla1,sla1)
                err = polFlow1.Next(self.srmplsTable)
                if err != nil {
                        log.Errorf("Error installing flow {%+v}. Err: %v", polFlow1, err)
                        return err
                }
		pRule := PolicyRule{
                        rule: rule,
                        flow: polFlow1,
                }
                self.Rules[rule.RuleId] = &pRule
		
		
	} else {
		log.Errorf("Unknown action in rule {%+v}", rule)
		return errors.New("Unknown action in rule")
	}
	return nil
}

// DelRule deletes a security rule from policy table
func (self *PolicyAgent) DelRule(rule *OfnetPolicyRule, ret *bool) error {
	log.Infof("Received DelRule: %+v", rule)

	// Gte the rule
	cache := self.Rules[rule.RuleId]
	if cache == nil {
		log.Errorf("Could not find rule: %+v", rule)
		return errors.New("rule not found")
	}

	// Delete the Flow
	err := cache.flow.Delete()
	if err != nil {
		log.Errorf("Error deleting flow: %+v. Err: %v", rule, err)
	}

	// Delete the rule from cache
	delete(self.Rules, rule.RuleId)

	return nil
}

// InitTables initializes policy table on the switch
func (self *PolicyAgent) InitTables(nextTblId uint8) error {
	sw := self.ofSwitch

	nextTbl := sw.GetTable(nextTblId)
	if nextTbl == nil {
		log.Fatalf("Error getting table id: %d", nextTblId)
	}

	self.nextTable = nextTbl

	// Create all tables
	self.dstGrpTable, _ = sw.NewTable(DST_GRP_TBL_ID)
	self.policyTable, _ = sw.NewTable(POLICY_TBL_ID)
	self.srmplsTable, _ = sw.NewTable(SRMPLS_TBL_ID)   //SRTE
	
 

	// Packets that miss dest group lookup still go to policy table
	validPktFlow, _ := self.dstGrpTable.NewFlow(ofctrl.FlowMatch{
		Priority: FLOW_MISS_PRIORITY,
	})
	validPktFlow.Next(self.policyTable)

	// Packets that didnt match any rule go to next table
	vlanMissFlow, _ := self.policyTable.NewFlow(ofctrl.FlowMatch{
		Priority: FLOW_MISS_PRIORITY,
	})
	vlanMissFlow.Next(nextTbl)

	return nil
}
