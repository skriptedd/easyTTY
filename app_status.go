package main

import (
	"easytty/internal/conn"
	"easytty/internal/status"
)

// ServerStatus reads load, memory, disk and top processes of the device behind
// an SSH tab (Linux or Cisco IOS) over separate exec channels. The detected
// device kind is cached per tab so later polls need only one round trip.
func (a *App) ServerStatus(tabID string) (status.Snapshot, error) {
	tc, err := a.terms.Conn(tabID)
	if err != nil {
		return status.Snapshot{}, err
	}
	sc, ok := tc.(*conn.SSHConn)
	if !ok {
		return status.Snapshot{}, errNoStatus
	}
	a.mu.Lock()
	kind := a.statusKind[tabID]
	a.mu.Unlock()
	snap, err := status.Collect(a.ctx, status.SSHExec(sc.Client()), kind)
	if err != nil {
		return snap, err
	}
	a.mu.Lock()
	a.statusKind[tabID] = snap.Kind
	a.mu.Unlock()
	return snap, nil
}
