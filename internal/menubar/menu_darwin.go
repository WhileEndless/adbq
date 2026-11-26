//go:build darwin && cgo && (dev || production)

package menubar

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>

void ADBQMountMenu(void);
void ADBQUnmountMenu(void);
*/
import "C"

import (
	"encoding/json"
	"sync"
)

const Supported = true

var nativeMenu struct {
	sync.Mutex
	snapshot  func() []Item
	callbacks map[int]func()
	nextID    int
}

// Mount installs the process's status item. AppKit owns all native UI work on
// the main queue. The snapshot callback must only read in-memory state.
func Mount(snapshot func() []Item) func() {
	nativeMenu.Lock()
	nativeMenu.snapshot = snapshot
	nativeMenu.Unlock()
	C.ADBQMountMenu()
	return func() {
		nativeMenu.Lock()
		nativeMenu.snapshot = nil
		nativeMenu.callbacks = nil
		nativeMenu.Unlock()
		C.ADBQUnmountMenu()
	}
}

type nativeItem struct {
	Item
	ID       int          `json:"id,omitempty"`
	Children []nativeItem `json:"children,omitempty"`
}

func nativeItems(items []Item) []nativeItem {
	result := make([]nativeItem, 0, len(items))
	for _, item := range items {
		row := nativeItem{Item: item, Children: nativeItems(item.Children)}
		if item.Run != nil && !item.Disabled {
			nativeMenu.nextID++
			row.ID = nativeMenu.nextID
			nativeMenu.callbacks[row.ID] = item.Run
		}
		result = append(result, row)
	}
	return result
}

//export ADBQMenuSnapshot
func ADBQMenuSnapshot() *C.char {
	nativeMenu.Lock()
	defer nativeMenu.Unlock()
	if nativeMenu.snapshot == nil {
		return nil
	}
	// IDs stay unique across snapshots: an AppKit item left over from a
	// prior menu refresh must never run a different device or app action.
	nativeMenu.callbacks = make(map[int]func())
	rows := nativeItems(nativeMenu.snapshot())
	data, err := json.Marshal(rows)
	if err != nil {
		return nil
	}
	return C.CString(string(data))
}

//export ADBQMenuClick
func ADBQMenuClick(id C.long) {
	nativeMenu.Lock()
	defer nativeMenu.Unlock()
	if run := nativeMenu.callbacks[int(id)]; run != nil {
		go run()
	}
}
