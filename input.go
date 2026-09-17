package main

import (
	"syscall"
)

const (
	wmKeyDown = 0x0100
	wmKeyUp   = 0x0101
)

var (
	user32DLL           = syscall.NewLazyDLL("user32.dll")
	procBotSendMessageW = user32DLL.NewProc("SendMessageW")
)

func pressKeyToWindow(hwnd uintptr, vk uintptr) bool {
	if hwnd == 0 || vk == 0 {
		return false
	}

	// Kathana consumes a posted down/up pair as normal text while its chat
	// editor is active. A synchronous WM_KEYDOWN is handled by the game's
	// command path instead: it still triggers skills, but does not inject the
	// key into the chat editor. Keep this as one message -- a synthetic key-up
	// is neither needed nor wanted for these one-shot actions.
	procBotSendMessageW.Call(
		hwnd,
		uintptr(wmKeyDown),
		vk,
		0,
	)

	return true
}

func scanCodeForVK(vk uintptr) uintptr {

	switch vk {

	case 0x0D: // ENTER
		return 0x1C

	case 0x45: // E
		return 0x12

	case 0x52: // R
		return 0x13

	case 0x46: // F
		return 0x21

	case 0x31: // 1
		return 0x02

	case 0x32: // 2
		return 0x03

	case 0x33: // 3
		return 0x04

	case 0x34: // 4
		return 0x05

	case 0x35: // 5
		return 0x06

	case 0x36: // 6
		return 0x07

	case 0x37: // 7
		return 0x08

	case 0x38: // 8
		return 0x09

	case 0x39: // 9
		return 0x0A

	case 0x30: // 0
		return 0x0B

	default:
		return 0
	}
}
