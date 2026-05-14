package graph

import (
	"strings"

	"github.com/Rafaelhdsg/safecut/internal/providers"
)

// LinkAzureResources inspects Azure resource properties and establishes
// real parent-child relationships in the dependency graph.
//
// Relationships discovered:
//   - Disk → VM   via properties.managedBy
//   - NIC  → VM   via properties.virtualMachine.id
//   - IP   → NIC  via properties.ipConfiguration.id
//
// Combined chain: VM → NIC → IP, VM → Disk
func LinkAzureResources(dg *DependencyGraph, resources []providers.Resource) {
	// Two passes: first link NIC→VM (so NICs have parents),
	// then link IP→NIC and Disk→VM.
	for _, r := range resources {
		if strings.Contains(strings.ToLower(r.Type), "microsoft.network/networkinterfaces") {
			linkNICToVM(dg, r)
		}
	}
	for _, r := range resources {
		t := strings.ToLower(r.Type)
		switch {
		case strings.Contains(t, "microsoft.compute/disks"):
			linkDiskToVM(dg, r)
		case strings.Contains(t, "microsoft.network/publicipaddresses"):
			linkIPToNIC(dg, r)
		}
	}
}

// linkDiskToVM: if a disk has properties.managedBy set, it's attached to a VM.
// managedBy contains the full ARM resource ID of the owning VM.
func linkDiskToVM(dg *DependencyGraph, disk providers.Resource) {
	managedBy, _ := disk.Properties["managedBy"].(string)
	if managedBy == "" {
		return
	}
	dg.Link(managedBy, disk.ID)
}

// linkNICToVM: if a NIC has properties.virtualMachine.id set, it's attached to a VM.
func linkNICToVM(dg *DependencyGraph, nic providers.Resource) {
	vm, ok := nic.Properties["virtualMachine"].(map[string]interface{})
	if !ok || vm == nil {
		return
	}
	vmID, _ := vm["id"].(string)
	if vmID == "" {
		return
	}
	dg.Link(vmID, nic.ID)
}

// linkIPToNIC: if a public IP has properties.ipConfiguration.id set,
// it's bound to a NIC's IP configuration (and thus in active use).
func linkIPToNIC(dg *DependencyGraph, ip providers.Resource) {
	ipConfig, ok := ip.Properties["ipConfiguration"].(map[string]interface{})
	if !ok || ipConfig == nil {
		return
	}
	configID, _ := ipConfig["id"].(string)
	if configID == "" {
		return
	}

	// ipConfiguration.id looks like:
	//   /subscriptions/.../networkInterfaces/{nic}/ipConfigurations/{name}
	// Extract the NIC resource ID (everything before /ipConfigurations/).
	idx := strings.Index(strings.ToLower(configID), "/ipconfigurations/")
	if idx < 0 {
		return
	}
	nicID := configID[:idx]
	dg.Link(nicID, ip.ID)
}
