package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const bifReturnOnlyFSDirs = 0x0001

type browseInfo struct {
	owner       windows.Handle
	root        uintptr
	displayName *uint16
	title       *uint16
	flags       uint32
	callback    uintptr
	lParam      uintptr
	image       int32
}

// pickGameFolder uses the native folder dialog so the local WebView never has
// to expose an arbitrary filesystem path to JavaScript.
func pickGameFolder() (string, bool, error) {
	shell32 := windows.NewLazySystemDLL("shell32.dll")
	ole32 := windows.NewLazySystemDLL("ole32.dll")
	browseForFolder := shell32.NewProc("SHBrowseForFolderW")
	pathFromIDList := shell32.NewProc("SHGetPathFromIDListW")
	freeIDList := ole32.NewProc("CoTaskMemFree")

	title, err := windows.UTF16PtrFromString("Select the game folder")
	if err != nil {
		return "", false, err
	}

	kaToolsWindowMu.RLock()
	owner := kaToolsWindow
	kaToolsWindowMu.RUnlock()
	info := browseInfo{owner: owner, title: title, flags: bifReturnOnlyFSDirs}
	idList, _, _ := browseForFolder.Call(uintptr(unsafe.Pointer(&info)))
	if idList == 0 {
		return "", false, nil
	}
	defer freeIDList.Call(idList)

	var path [260]uint16
	ok, _, _ := pathFromIDList.Call(idList, uintptr(unsafe.Pointer(&path[0])))
	if ok == 0 {
		return "", false, fmt.Errorf("read selected game folder")
	}
	return windows.UTF16ToString(path[:]), true, nil
}
