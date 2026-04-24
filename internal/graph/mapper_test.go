package graph

import "testing"

func TestDependencyGraph_LinkAndDependents(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNode(&Node{ID: "vm-1", Name: "vm", Type: "Microsoft.Compute/virtualMachines"})
	g.AddNode(&Node{ID: "disk-1", Name: "osdisk", Type: "Microsoft.Compute/disks"})
	if !g.Link("vm-1", "disk-1") {
		t.Fatal("link failed")
	}
	if !g.HasDependents("vm-1") {
		t.Fatal("vm should have dependents after linking disk")
	}
	if g.HasDependents("disk-1") {
		t.Fatal("disk has no children")
	}
}

func TestDependencyGraph_Orphans(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNode(&Node{ID: "orphan", Name: "orphan-ip", Type: "Microsoft.Network/publicIPAddresses"})
	g.AddNode(&Node{ID: "vm-a", Name: "vm-a", Type: "Microsoft.Compute/virtualMachines"})
	g.AddNode(&Node{ID: "disk-a", Name: "disk-a", Type: "Microsoft.Compute/disks"})
	g.Link("vm-a", "disk-a")

	orphans := g.Orphans()
	if len(orphans) != 1 || orphans[0].ID != "orphan" {
		t.Fatalf("expected single orphan IP, got %+v", orphans)
	}
}

func TestDependencyGraph_caseInsensitiveLookup(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNode(&Node{ID: "/Sub/ABC", Name: "n"})
	if _, ok := g.GetNode("/sub/abc"); !ok {
		t.Fatal("expected case-insensitive lookup to succeed")
	}
}
