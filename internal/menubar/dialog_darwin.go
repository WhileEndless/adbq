//go:build darwin && cgo && (dev || production)

package menubar

/*
#include <stdint.h>
#include <stdlib.h>

void ADBQShowDialog(uintptr_t id, const char *title, const char *message,
                    const char *dismiss, const char *action, int destructive);
void ADBQCancelDialog(uintptr_t id);
*/
import "C"

import (
	"context"
	"errors"
	"runtime/cgo"
	"unsafe"
)

// ShowDialog waits for a menu bar popup from a background goroutine. It
// returns true only when the user chooses the secondary action.
func ShowDialog(ctx context.Context, dialog Dialog) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	response := make(chan dialogResponse, 1)
	handle := cgo.NewHandle(response)
	id := C.uintptr_t(handle)
	title, message := C.CString(dialog.Title), C.CString(dialog.Message)
	dismiss, action := C.CString(dialog.Dismiss), C.CString(dialog.Action)
	var destructive C.int
	if dialog.Destructive {
		destructive = 1
	}
	C.ADBQShowDialog(id, title, message, dismiss, action, destructive)
	C.free(unsafe.Pointer(title))
	C.free(unsafe.Pointer(message))
	C.free(unsafe.Pointer(dismiss))
	C.free(unsafe.Pointer(action))

	select {
	case <-ctx.Done():
		C.ADBQCancelDialog(id)
		return false, ctx.Err()
	case result := <-response:
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if result == dialogBusy {
			return false, errors.New("close the open adbq popup and try again")
		}
		return result == dialogAccepted, nil
	}
}

//export ADBQDialogResponse
func ADBQDialogResponse(id C.uintptr_t, response C.int) {
	handle := cgo.Handle(id)
	reply := handle.Value().(chan dialogResponse)
	handle.Delete()
	reply <- dialogResponse(response)
}
