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
package ofctrl

// This file implements the forwarding graph API for the flow

import (
	"encoding/json"
	"net"
	"errors"

	log "github.com/Sirupsen/logrus"
	"github.com/shaleman/libOpenflow/openflow13"
)

// Small subset of openflow fields we currently support
type FlowMatch struct {
	Priority     uint16            // Priority of the flow
	InputPort    uint32            // Input port number
	MacDa        *net.HardwareAddr // Mac dest
	MacDaMask    *net.HardwareAddr // Mac dest mask
	MacSa        *net.HardwareAddr // Mac source
	MacSaMask    *net.HardwareAddr // Mac source mask
	Ethertype    uint16            // Ethertype
	VlanId       uint16            // vlan id
	VlanIdMask	 *uint16 			   // vlan mask -SRTE
	MplsLabel    uint32		   	   // Mpls Label  - SRTE
	MplsBos      uint8		   	   // Mpls Bos  - SRTE
	IpSa         *net.IP
	IpSaMask     *net.IP
	IpDa         *net.IP
	IpDaMask     *net.IP
	IpProto      uint8
	TcpSrcPort   uint16
	TcpDstPort   uint16
	UdpSrcPort   uint16
	UdpDstPort   uint16
	Metadata     *uint64
	MetadataMask *uint64
	TunnelId     uint64  // Vxlan Tunnel id i.e. VNI
	TcpFlags     *uint16 // TCP flags
	TcpFlagsMask *uint16 // Mask for TCP flags
}

// additional actions in flow's instruction set
type FlowAction struct {
	actionType   string           // Type of action "setVlan", "setMetadata"
	vlanId       uint16           // Vlan Id in case of "setVlan"
	mplsLabel    uint32			  // mpls label - SRTE
	macAddr      net.HardwareAddr // Mac address to set
	ipAddr       net.IP           // IP address to be set
	l4Port       uint16           // Transport port to be set
	tunnelId     uint64           // Tunnel Id (used for setting VNI)
	metadata     uint64           // Metadata in case of "setMetadata"
	metadataMask uint64           // Metadata mask
}

// State of a flow entry
type Flow struct {
	Table       *Table        // Table where this flow resides
	Match       FlowMatch     // Fields to be matched
	NextElem    FgraphElem    // Next fw graph element
	isInstalled bool          // Is the flow installed in the switch
	flowId      uint64        // Unique ID for the flow
	flowActions []*FlowAction // List of flow actions
}

const IP_PROTO_TCP = 6
const IP_PROTO_UDP = 17

// string key for the flow
// FIXME: simple json conversion for now. This needs to be smarter
func (self *Flow) flowKey() string {
	jsonVal, err := json.Marshal(self.Match)
	if err != nil {
		log.Errorf("Error forming flowkey for %+v. Err: %v", self, err)
		return ""
	}

	return string(jsonVal)
}

// Fgraph element type for the flow
func (self *Flow) Type() string {
	return "flow"
}

// instruction set for flow element
func (self *Flow) GetFlowInstr() openflow13.Instruction {
	log.Fatalf("Unexpected call to get flow's instruction set")
	return nil
}

// Translate our match fields into openflow 1.3 match fields
func (self *Flow) xlateMatch() openflow13.Match {
	ofMatch := openflow13.NewMatch()

	// Handle input poty
	if self.Match.InputPort != 0 {
		inportField := openflow13.NewInPortField(self.Match.InputPort)
		ofMatch.AddField(*inportField)
	}

	// Handle mac DA field
	if self.Match.MacDa != nil {
		if self.Match.MacDaMask != nil {
			macDaField := openflow13.NewEthDstField(*self.Match.MacDa, self.Match.MacDaMask)
			ofMatch.AddField(*macDaField)
		} else {
			macDaField := openflow13.NewEthDstField(*self.Match.MacDa, nil)
			ofMatch.AddField(*macDaField)
		}
	}

	// Handle MacSa field
	if self.Match.MacSa != nil {
		if self.Match.MacSaMask != nil {
			macSaField := openflow13.NewEthSrcField(*self.Match.MacSa, self.Match.MacSaMask)
			ofMatch.AddField(*macSaField)
		} else {
			macSaField := openflow13.NewEthSrcField(*self.Match.MacSa, nil)
			ofMatch.AddField(*macSaField)
		}
	}

	// Handle ethertype
	if self.Match.Ethertype != 0 {
		etypeField := openflow13.NewEthTypeField(self.Match.Ethertype)
		ofMatch.AddField(*etypeField)
	}

	// Handle Vlan id
	if self.Match.VlanId != 0  {
		if self.Match.VlanIdMask != nil {
			vidField := openflow13.NewVlanIdField(self.Match.VlanId, self.Match.VlanIdMask )
			ofMatch.AddField(*vidField)
		} else {
			vidField := openflow13.NewVlanIdField(self.Match.VlanId, nil )
			ofMatch.AddField(*vidField)
		}
	}


	// Handle MPLS Label -SRTE
	if self.Match.MplsLabel != 0 {
		mplsLabelField := openflow13.NewMplsLabelField(self.Match.MplsLabel)
		ofMatch.AddField(*mplsLabelField)
	}

	// Handle MPLS Bos -SRTE
	if self.Match.MplsBos != 0 {
		mplsBosField := openflow13.NewMplsBosField(self.Match.MplsBos)
		ofMatch.AddField(*mplsBosField)
	}

	// Handle IP Dst
	if self.Match.IpDa != nil {
		if self.Match.IpDaMask != nil {
			ipDaField := openflow13.NewIpv4DstField(*self.Match.IpDa, self.Match.IpDaMask)
			ofMatch.AddField(*ipDaField)
		} else {
			ipDaField := openflow13.NewIpv4DstField(*self.Match.IpDa, nil)
			ofMatch.AddField(*ipDaField)
		}
	}

	// Handle IP Src
	if self.Match.IpSa != nil {
		if self.Match.IpSaMask != nil {
			ipSaField := openflow13.NewIpv4SrcField(*self.Match.IpSa, self.Match.IpSaMask)
			ofMatch.AddField(*ipSaField)
		} else {
			ipSaField := openflow13.NewIpv4SrcField(*self.Match.IpSa, nil)
			ofMatch.AddField(*ipSaField)
		}
	}

	// Handle IP protocol
	if self.Match.IpProto != 0 {
		protoField := openflow13.NewIpProtoField(self.Match.IpProto)
		ofMatch.AddField(*protoField)
	}

	// Handle port numbers
	if self.Match.IpProto == IP_PROTO_TCP && self.Match.TcpSrcPort != 0 {
		portField := openflow13.NewTcpSrcField(self.Match.TcpSrcPort)
		ofMatch.AddField(*portField)
	}
	if self.Match.IpProto == IP_PROTO_TCP && self.Match.TcpDstPort != 0 {
		portField := openflow13.NewTcpDstField(self.Match.TcpDstPort)
		ofMatch.AddField(*portField)
	}
	if self.Match.IpProto == IP_PROTO_UDP && self.Match.UdpSrcPort != 0 {
		portField := openflow13.NewUdpSrcField(self.Match.UdpSrcPort)
		ofMatch.AddField(*portField)
	}
	if self.Match.IpProto == IP_PROTO_UDP && self.Match.UdpDstPort != 0 {
		portField := openflow13.NewUdpDstField(self.Match.UdpDstPort)
		ofMatch.AddField(*portField)
	}

	// Handle tcp flags
	if self.Match.IpProto == IP_PROTO_TCP && self.Match.TcpFlags != nil {
		tcpFlagField := openflow13.NewTcpFlagsField(*self.Match.TcpFlags, self.Match.TcpFlagsMask)
		ofMatch.AddField(*tcpFlagField)
	}

	// Handle metadata
	if self.Match.Metadata != nil {
		if self.Match.MetadataMask != nil {
			metadataField := openflow13.NewMetadataField(*self.Match.Metadata, self.Match.MetadataMask)
			ofMatch.AddField(*metadataField)
		} else {
			metadataField := openflow13.NewMetadataField(*self.Match.Metadata, nil)
			ofMatch.AddField(*metadataField)
		}
	}

	// Handle Vxlan tunnel id
	if self.Match.TunnelId != 0 {
		tunnelIdField := openflow13.NewTunnelIdField(self.Match.TunnelId)
		ofMatch.AddField(*tunnelIdField)
	}

	return *ofMatch
}

// Install all flow actions
func (self *Flow) installFlowActions(flowMod *openflow13.FlowMod,
	instr openflow13.Instruction) error {
	var actInstr openflow13.Instruction
	var addActn bool = false

	// Create a apply_action instruction to be used if its not already created
	switch instr.(type) {
	case *openflow13.InstrActions:
		actInstr = instr
	default:
		actInstr = openflow13.NewInstrApplyActions()
	}

	// Loop thru all actions
	for _, flowAction := range self.flowActions {
		switch flowAction.actionType {
		case "pushMpls": //SRTE
			// Push mpls Tag action
			pushMplsAction := openflow13.NewActionPushMpls(0x8847)

			// Set Outer mpls label field
			mplsLabelField := openflow13.NewMplsLabelField(flowAction.mplsLabel)
			setMplsAction := openflow13.NewActionSetField(*mplsLabelField)

			// Prepend push mpls & setlabel actions to existing instruction
			actInstr.AddAction(setMplsAction, true)
			actInstr.AddAction(pushMplsAction, true)
						
			addActn = true

			log.Debugf("flow install. Added mpls action: %+v, setMpls actions: %+v",
				pushMplsAction, setMplsAction)
		case "popVlanPushMpls": //SRTE
			// Push mpls Tag action
			pushMplsAction := openflow13.NewActionPushMpls(0x8847)

			// Set Outer mpls label field
			mplsLabelField := openflow13.NewMplsLabelField(flowAction.mplsLabel)
			setMplsAction := openflow13.NewActionSetField(*mplsLabelField)

			// Prepend push mpls & setlabel actions to existing instruction
			actInstr.AddAction(setMplsAction, true)
			actInstr.AddAction(pushMplsAction, true)

			// Create pop vlan action 
			popVlan := openflow13.NewActionPopVlan()

			// Add it to instruction
			actInstr.AddAction(popVlan, true)
				
			addActn = true

			log.Debugf("flow install. Added pop vlan action: %+v,  and Added mpls action: %+v, setMpls actions: %+v",
				popVlan, pushMplsAction, setMplsAction)
		case "popMplsPushVlan": //SRTE
			// Push Vlan Tag action
			pushVlanAction := openflow13.NewActionPushVlan(0x8100)

			// Set Outer vlan tag field
			vlanField := openflow13.NewVlanIdField(flowAction.vlanId, nil)
			setVlanAction := openflow13.NewActionSetField(*vlanField)

			// Prepend push vlan & setvlan actions to existing instruction
			actInstr.AddAction(setVlanAction, true)
			actInstr.AddAction(pushVlanAction, true)

			//popmpls action
			popMplsAction := openflow13.NewActionPopMpls(0x0800)
			
			actInstr.AddAction(popMplsAction, true)
			
			addActn = true
			log.Debugf("flow install. Added pop mpls action: %+v,  and Added push vlan action: %+v, setVlan actions: %+v",
				popMplsAction, setVlanAction, vlanField)
		case "swapMpls": //SRTE - Test this 
			// Set Outer mpls label field
			mplsLabelField := openflow13.NewMplsLabelField(flowAction.mplsLabel)
			setMplsAction := openflow13.NewActionSetField(*mplsLabelField)

			// Prepend push mpls & setlabel actions to existing instruction
			actInstr.AddAction(setMplsAction, true)
			
			
			addActn = true

			log.Debugf("flow install. Added swap mpls - setMpls actions: %+v", setMplsAction)
		case "popMpls": //SRTE
			// Pop mpls Tag action
			popMplsAction := openflow13.NewActionPopMpls(0x0800)
			
			actInstr.AddAction(popMplsAction, true)
	
			addActn = true
		
			log.Debugf("flow install. Pop mpls action: %+v",
				popMplsAction)
		case "setVlan":
			// Push Vlan Tag action
			pushVlanAction := openflow13.NewActionPushVlan(0x8100)

			// Set Outer vlan tag field
			vlanField := openflow13.NewVlanIdField(flowAction.vlanId, nil)
			setVlanAction := openflow13.NewActionSetField(*vlanField)

			// Prepend push vlan & setvlan actions to existing instruction
			actInstr.AddAction(setVlanAction, true)
			actInstr.AddAction(pushVlanAction, true)
			addActn = true

			log.Debugf("flow install. Added pushvlan action: %+v, setVlan actions: %+v",
				pushVlanAction, setVlanAction)

		case "popVlan":
			// Create pop vln action
			popVlan := openflow13.NewActionPopVlan()

			// Add it to instruction
			actInstr.AddAction(popVlan, true)
			addActn = true

			log.Debugf("flow install. Added popVlan action: %+v", popVlan)

		case "setMacDa":
			// Set Outer MacDA field
			macDaField := openflow13.NewEthDstField(flowAction.macAddr, nil)
			setMacDaAction := openflow13.NewActionSetField(*macDaField)

			// Add set macDa action to the instruction
			actInstr.AddAction(setMacDaAction, true)
			addActn = true

			log.Debugf("flow install. Added setMacDa action: %+v", setMacDaAction)

		case "setMacSa":
			// Set Outer MacSA field
			macSaField := openflow13.NewEthSrcField(flowAction.macAddr, nil)
			setMacSaAction := openflow13.NewActionSetField(*macSaField)

			// Add set macDa action to the instruction
			actInstr.AddAction(setMacSaAction, true)
			addActn = true

			log.Debugf("flow install. Added setMacSa Action: %+v", setMacSaAction)

		case "setTunnelId":
			// Set tunnelId field
			tunnelIdField := openflow13.NewTunnelIdField(flowAction.tunnelId)
			setTunnelAction := openflow13.NewActionSetField(*tunnelIdField)

			// Add set tunnel action to the instruction
			actInstr.AddAction(setTunnelAction, true)
			addActn = true

			log.Debugf("flow install. Added setTunnelId Action: %+v", setTunnelAction)

		case "setMetadata":
			// Set Metadata instruction
			metadataInstr := openflow13.NewInstrWriteMetadata(flowAction.metadata, flowAction.metadataMask)

			// Add the instruction to flowmod
			flowMod.AddInstruction(metadataInstr)

		case "setIPSa":
			// Set IP src
			ipSaField := openflow13.NewIpv4SrcField(flowAction.ipAddr, nil)
			setIPSaAction := openflow13.NewActionSetField(*ipSaField)

			// Add set action to the instruction
			actInstr.AddAction(setIPSaAction, true)
			addActn = true

			log.Debugf("flow install. Added setIPSa Action: %+v", setIPSaAction)

		case "setIPDa":
			// Set IP dst
			ipDaField := openflow13.NewIpv4DstField(flowAction.ipAddr, nil)
			setIPDaAction := openflow13.NewActionSetField(*ipDaField)

			// Add set action to the instruction
			actInstr.AddAction(setIPDaAction, true)
			addActn = true

			log.Debugf("flow install. Added setIPDa Action: %+v", setIPDaAction)

		case "setTCPSrc":
			// Set TCP src
			tcpSrcField := openflow13.NewTcpSrcField(flowAction.l4Port)
			setTCPSrcAction := openflow13.NewActionSetField(*tcpSrcField)

			// Add set action to the instruction
			actInstr.AddAction(setTCPSrcAction, true)
			addActn = true

			log.Debugf("flow install. Added setTCPSrc Action: %+v", setTCPSrcAction)

		case "setTCPDst":
			// Set TCP dst
			tcpDstField := openflow13.NewTcpDstField(flowAction.l4Port)
			setTCPDstAction := openflow13.NewActionSetField(*tcpDstField)

			// Add set action to the instruction
			actInstr.AddAction(setTCPDstAction, true)
			addActn = true

			log.Debugf("flow install. Added setTCPDst Action: %+v", setTCPDstAction)

		case "setUDPSrc":
			// Set UDP src
			udpSrcField := openflow13.NewUdpSrcField(flowAction.l4Port)
			setUDPSrcAction := openflow13.NewActionSetField(*udpSrcField)

			// Add set action to the instruction
			actInstr.AddAction(setUDPSrcAction, true)
			addActn = true

			log.Debugf("flow install. Added setUDPSrc Action: %+v", setUDPSrcAction)

		case "setUDPDst":
			// Set UDP dst
			udpDstField := openflow13.NewUdpDstField(flowAction.l4Port)
			setUDPDstAction := openflow13.NewActionSetField(*udpDstField)

			// Add set action to the instruction
			actInstr.AddAction(setUDPDstAction, true)
			addActn = true

			log.Debugf("flow install. Added setUDPDst Action: %+v", setUDPDstAction)

		default:
			log.Fatalf("Unknown action type %s", flowAction.actionType)
		}
	}

	// Add the instruction to flow if its not already added
	if (addActn) && (actInstr != instr) {
		// Add the instrction to flowmod
		flowMod.AddInstruction(actInstr)
	}

	return nil
}

// Install a flow entry
func (self *Flow) install() error {
	// Create a flowmode entry
	flowMod := openflow13.NewFlowMod()
	flowMod.TableId = self.Table.TableId
	flowMod.Priority = self.Match.Priority
	flowMod.Cookie = self.flowId

	// Add or modify
	if !self.isInstalled {
		flowMod.Command = openflow13.FC_ADD
	} else {
		flowMod.Command = openflow13.FC_MODIFY
	}

	// convert match fields to openflow 1.3 format
	flowMod.Match = self.xlateMatch()
	log.Debugf("flow install: Match: %+v", flowMod.Match)

	// Based on the next elem, decide what to install
	switch self.NextElem.Type() {
	case "table":
		// Get the instruction set from the element
		instr := self.NextElem.GetFlowInstr()

		// Check if there are any flow actions to perform
		self.installFlowActions(flowMod, instr)

		// Add the instruction to flowmod
		flowMod.AddInstruction(instr)

		log.Debugf("flow install: added goto table instr: %+v", instr)

	case "flood":
		fallthrough
	case "output":
		// Get the instruction set from the element
		instr := self.NextElem.GetFlowInstr()

		// Add the instruction to flowmod if its not nil
		// a nil instruction means drop action
		if instr != nil {

			// Check if there are any flow actions to perform
			self.installFlowActions(flowMod, instr)

			flowMod.AddInstruction(instr)

			log.Debugf("flow install: added output port instr: %+v", instr)
		}
	default:
		log.Fatalf("Unknown Fgraph element type %s", self.NextElem.Type())
	}

	log.Debugf("Sending flowmod: %+v", flowMod)

	// Send the message
	self.Table.Switch.Send(flowMod)

	// Mark it as installed
	self.isInstalled = true

	return nil
}

// Set Next element in the Fgraph. This determines what actions will be
// part of the flow's instruction set
func (self *Flow) Next(elem FgraphElem) error {
	// Set the next element in the graph
	self.NextElem = elem

	// Install the flow entry
	return self.install()
}
// Special actions on the flow to pop mpls and push vlan -SRTE
func (self *Flow) PopMplsPushVlan(vlanId uint16) error {
	action := new(FlowAction)
	action.actionType = "popMplsPushVlan"
	action.vlanId = vlanId

	self.flowActions = append(self.flowActions, action)

	// If the flow entry was already installed, re-install it
	if self.isInstalled {
		self.install()
	}
	return nil 
}


// Special actions on the flow to pop vlan and push mpls -SRTE
func (self *Flow) PopVlanPushMpls(mplsLabel uint32) error {
	action := new(FlowAction)
	action.actionType = "popVlanPushMpls"
	action.mplsLabel = mplsLabel

	// Add to the action list
	self.flowActions = append(self.flowActions, action)

	// If the flow entry was already installed, re-install it
	if self.isInstalled {
		self.install()
	}
	return nil 
}

// Special actions on the flow to push mpls -SRTE
func (self *Flow) PushMpls(mplsLabel uint32) error {
	action := new(FlowAction)
	action.actionType = "pushMpls"
	action.mplsLabel = mplsLabel

	// Add to the action list
	self.flowActions = append(self.flowActions, action)

	// If the flow entry was already installed, re-install it
	if self.isInstalled {
		self.install()
	}

	return nil 
}

// Special actions on the flow to swap mpls label -SRTE
func (self *Flow) SwapMpls(mplsLabel uint32) error {
	action := new(FlowAction)
	action.actionType = "swapMpls"
	action.mplsLabel = mplsLabel

	// Add to the action list
	self.flowActions = append(self.flowActions, action)

	// If the flow entry was already installed, re-install it
	if self.isInstalled {
		self.install()
	}

	return nil 
}

// Special actions on the flow to pop Mpls
func (self *Flow) PopMpls() error {
	action := new(FlowAction)
	action.actionType = "popMpls"

	self.flowActions = append(self.flowActions, action)

	// If the flow entry was already installed, re-install it
	if self.isInstalled {
		self.install()
	}

	return nil
}


// Special actions on the flow to set vlan id
func (self *Flow) SetVlan(vlanId uint16) error {
	action := new(FlowAction)
	action.actionType = "setVlan"
	action.vlanId = vlanId

	// Add to the action list
	// FIXME: detect duplicates
	self.flowActions = append(self.flowActions, action)

	// If the flow entry was already installed, re-install it
	if self.isInstalled {
		self.install()
	}

	return nil
}

// Special actions on the flow to set vlan id
func (self *Flow) PopVlan() error {
	action := new(FlowAction)
	action.actionType = "popVlan"

	// Add to the action list
	// FIXME: detect duplicates
	self.flowActions = append(self.flowActions, action)

	// If the flow entry was already installed, re-install it
	if self.isInstalled {
		self.install()
	}

	return nil
}

// Special actions on the flow to set mac dest addr
func (self *Flow) SetMacDa(macDa net.HardwareAddr) error {
	action := new(FlowAction)
	action.actionType = "setMacDa"
	action.macAddr = macDa

	// Add to the action list
	// FIXME: detect duplicates
	self.flowActions = append(self.flowActions, action)

	// If the flow entry was already installed, re-install it
	if self.isInstalled {
		self.install()
	}

	return nil
}

// Special action on the flow to set mac source addr
func (self *Flow) SetMacSa(macSa net.HardwareAddr) error {
	action := new(FlowAction)
	action.actionType = "setMacSa"
	action.macAddr = macSa

	// Add to the action list
	// FIXME: detect duplicates
	self.flowActions = append(self.flowActions, action)

	// If the flow entry was already installed, re-install it
	if self.isInstalled {
		self.install()
	}

	return nil
}

// Special action on the flow to set an ip field
func (self *Flow) SetIPField(ip net.IP, field string) error {
	action := new(FlowAction)
	if field == "Src" {
		action.actionType = "setIPSa"
	} else if field == "Dst" {
		action.actionType = "setIPDa"
	} else {
		return errors.New("field not supported")
	}

	action.ipAddr = ip
	// Add to the action list
	self.flowActions = append(self.flowActions, action)

	// If the flow entry was already installed, re-install it
	if self.isInstalled {
		self.install()
	}

	return nil
}

// Special action on the flow to set a L4 field
func (self *Flow) SetL4Field(port uint16, field string) error {
	action := new(FlowAction)

	switch field {
	case "TCPSrc":
		action.actionType = "setTCPSrc"
		break
	case "TCPDst":
		action.actionType = "setTCPDst"
		break
	case "UDPSrc":
		action.actionType = "setUDPSrc"
		break
	case "UDPDst":
		action.actionType = "setUDPDst"
		break
	default:
		return errors.New("field not supported")
	}

	action.l4Port = port
	// Add to the action list
	self.flowActions = append(self.flowActions, action)

	// If the flow entry was already installed, re-install it
	if self.isInstalled {
		self.install()
	}

	return nil
}

// Special actions on the flow to set metadata
func (self *Flow) SetMetadata(metadata, metadataMask uint64) error {
	action := new(FlowAction)
	action.actionType = "setMetadata"
	action.metadata = metadata
	action.metadataMask = metadataMask

	// Add to the action list
	// FIXME: detect duplicates
	self.flowActions = append(self.flowActions, action)

	// If the flow entry was already installed, re-install it
	if self.isInstalled {
		self.install()
	}

	return nil
}

// Special actions on the flow to set vlan id
func (self *Flow) SetTunnelId(tunnelId uint64) error {
	action := new(FlowAction)
	action.actionType = "setTunnelId"
	action.tunnelId = tunnelId

	// Add to the action list
	// FIXME: detect duplicates
	self.flowActions = append(self.flowActions, action)

	// If the flow entry was already installed, re-install it
	if self.isInstalled {
		self.install()
	}

	return nil
}

// Delete the flow
func (self *Flow) Delete() error {
	// Delete from ofswitch
	if self.isInstalled {
		// Create a flowmode entry
		flowMod := openflow13.NewFlowMod()
		flowMod.Command = openflow13.FC_DELETE
		flowMod.TableId = self.Table.TableId
		flowMod.Priority = self.Match.Priority
		flowMod.Cookie = self.flowId
		flowMod.CookieMask = 0xffffffffffffffff
		flowMod.OutPort = openflow13.P_ANY
		flowMod.OutGroup = openflow13.OFPG_ANY

		log.Debugf("Sending DELETE flowmod: %+v", flowMod)

		// Send the message
		self.Table.Switch.Send(flowMod)
	}

	// Delete it from the table
	flowKey := self.flowKey()
	self.Table.DeleteFlow(flowKey)

	return nil
}
