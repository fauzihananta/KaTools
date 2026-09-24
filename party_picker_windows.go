package main

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"KaTools/tools/ocrworker"

	"golang.org/x/sys/windows"
)

const (
	partyPickerClassName = "KaToolsPartyROIPicker"

	wsPopup        = 0x80000000
	wsExToolWindow = 0x00000080
	wsExTopmost    = 0x00000008

	wmPaint       = 0x000F
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmMouseMove   = 0x0200
	wmRButtonDown = 0x0204

	vkEscape = 0x1B

	swShow = 5

	swpNoActivate = 0x0010
	swpShowWindow = 0x0040

	wsExLayered = 0x00080000

	lwaAlpha = 0x00000002

	psSolid = 0

	colorRed = 0x000000FF
)

type pickerRECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type pickerPOINT struct {
	X int32
	Y int32
}

type pickerPAINTSTRUCT struct {
	Hdc         uintptr
	Erase       int32
	RcPaint     pickerRECT
	Restore     int32
	IncUpdate   int32
	RgbReserved [32]byte
}

type pickerMSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      pickerPOINT
}

type pickerWNDCLASSEX struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type partyPickerState struct {
	hwnd windows.Handle
	kind string

	targetHWND windows.Handle
	targetRect pickerRECT

	mu sync.Mutex

	dragging bool

	startX int
	startY int

	currentX int
	currentY int

	// A desktop preview must be captured after the translucent picker has been
	// destroyed, but before the dashboard is brought back to the foreground.
	// Otherwise BitBlt sees KaTools at the selected desktop coordinates instead
	// of the game the user just selected from.
	captureEvidence bool
	selectedROI     ocrworker.PartyROIConfig
}

var (
	partyPickerMu        sync.Mutex
	activePicker         *partyPickerState
	partyPickerClassOnce sync.Once
	partyPickerClassErr  error
	kaToolsWindowMu      sync.RWMutex
	kaToolsWindow        windows.Handle

	// Windows retains this callback pointer for the lifetime of the registered
	// class. It must not be a local variable in runPartyROIPicker, otherwise a
	// later picker window can call a callback that Go has already released.
	partyPickerWndProcCallback = syscall.NewCallback(partyPickerWndProc)
)

func openPartyROIPicker(targetHWND windows.Handle) error {
	return openROIPicker(targetHWND, "party")
}

func openStatusROIPicker(targetHWND windows.Handle) error {
	return openROIPicker(targetHWND, "status")
}

func openDeathROIPicker(targetHWND windows.Handle) error {
	return openROIPicker(targetHWND, "death")
}

func openTargetROIPicker(targetHWND windows.Handle) error {
	return openROIPicker(targetHWND, "target")
}

func openROIPicker(targetHWND windows.Handle, kind string) error {

	if targetHWND == 0 {
		return fmt.Errorf("invalid target window")
	}

	if !isWindowValid(uintptr(targetHWND)) {
		return fmt.Errorf("target window no longer exists")
	}

	// The picker is started by a click in KaTools. Bring the selected game to
	// the foreground first, so the overlay is immediately usable without an
	// Alt+Tab step.
	focusPickerTargetWindow(targetHWND)

	rect, err := getPickerWindowRect(
		uintptr(targetHWND),
	)

	if err != nil {
		return err
	}

	width := int(rect.Right - rect.Left)
	height := int(rect.Bottom - rect.Top)

	if width <= 0 || height <= 0 {
		return fmt.Errorf(
			"target window has invalid size",
		)
	}

	partyPickerMu.Lock()

	if activePicker != nil {
		partyPickerMu.Unlock()

		return fmt.Errorf(
			"party ROI picker is already open",
		)
	}

	state := &partyPickerState{
		targetHWND: targetHWND,
		targetRect: rect,
		kind:       kind,
	}

	activePicker = state

	partyPickerMu.Unlock()

	go runPartyROIPicker(state)

	return nil
}

func focusPickerTargetWindow(targetHWND windows.Handle) {
	focusWindow(targetHWND)
}

// setKaToolsWindow records the native dashboard window while it is alive. The
// ROI picker uses this to return the user to KaTools after its overlay closes.
func setKaToolsWindow(hwnd windows.Handle) {
	kaToolsWindowMu.Lock()
	kaToolsWindow = hwnd
	kaToolsWindowMu.Unlock()
}

func refocusKaToolsWindow() {
	kaToolsWindowMu.RLock()
	hwnd := kaToolsWindow
	kaToolsWindowMu.RUnlock()

	if hwnd == 0 || !isWindowValid(uintptr(hwnd)) {
		return
	}

	focusWindow(hwnd)
}

func focusWindow(targetHWND windows.Handle) {
	// Windows normally blocks a background process from stealing focus. The
	// picker was explicitly requested by the user, so temporarily join the
	// foreground input queue before asking Windows to activate its window.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	user32 := windows.NewLazySystemDLL("user32.dll")
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	showWindow := user32.NewProc("ShowWindow")
	setForegroundWindow := user32.NewProc("SetForegroundWindow")
	bringWindowToTop := user32.NewProc("BringWindowToTop")
	getForegroundWindow := user32.NewProc("GetForegroundWindow")
	getWindowThreadProcessID := user32.NewProc("GetWindowThreadProcessId")
	attachThreadInput := user32.NewProc("AttachThreadInput")
	getCurrentThreadID := kernel32.NewProc("GetCurrentThreadId")

	currentThread, _, _ := getCurrentThreadID.Call()
	foregroundHWND, _, _ := getForegroundWindow.Call()
	foregroundThread := uintptr(0)
	if foregroundHWND != 0 {
		foregroundThread, _, _ = getWindowThreadProcessID.Call(foregroundHWND, 0)
	}
	if currentThread != 0 && foregroundThread != 0 && currentThread != foregroundThread {
		attachThreadInput.Call(currentThread, foregroundThread, 1)
		defer attachThreadInput.Call(currentThread, foregroundThread, 0)
	}

	// SW_RESTORE also restores a minimized picker target before focusing it.
	for attempt := 0; attempt < 3; attempt++ {
		showWindow.Call(uintptr(targetHWND), 9)
		bringWindowToTop.Call(uintptr(targetHWND))
		setForegroundWindow.Call(uintptr(targetHWND))
		time.Sleep(60 * time.Millisecond)
	}
}

func isPartyROIPickerOpen() bool {
	partyPickerMu.Lock()
	defer partyPickerMu.Unlock()

	return activePicker != nil
}

func runPartyROIPicker(
	state *partyPickerState,
) {
	// A Win32 window and its message queue must stay on the OS thread that
	// created it. Without this, a later Go scheduling move can make a second
	// picker unresponsive or terminate the process during a window callback.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	defer func() {
		partyPickerMu.Lock()

		if activePicker == state {
			activePicker = nil
		}

		partyPickerMu.Unlock()

		capturePickerEvidence(state)

		// The picker temporarily activates Kathana so the user can select an
		// area. Return to the dashboard only after the selected game pixels have
		// been captured for the preview and WGC comparison.
		refocusKaToolsWindow()
	}()

	user32 := windows.NewLazySystemDLL("user32.dll")
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")

	registerClassEx := user32.NewProc(
		"RegisterClassExW",
	)

	createWindowEx := user32.NewProc(
		"CreateWindowExW",
	)

	setWindowPos := user32.NewProc(
		"SetWindowPos",
	)

	showWindow := user32.NewProc(
		"ShowWindow",
	)

	updateWindow := user32.NewProc(
		"UpdateWindow",
	)

	getMessage := user32.NewProc(
		"GetMessageW",
	)

	translateMessage := user32.NewProc(
		"TranslateMessage",
	)

	dispatchMessage := user32.NewProc(
		"DispatchMessageW",
	)

	getModuleHandle := kernel32.NewProc(
		"GetModuleHandleW",
	)

	instance, _, _ :=
		getModuleHandle.Call(0)

	className, err :=
		windows.UTF16PtrFromString(
			partyPickerClassName,
		)

	if err != nil {
		fmt.Println(
			"[Party Picker] class name error:",
			err,
		)
		return
	}

	cursor := loadPickerCursor()

	if err := ensurePartyPickerClass(
		registerClassEx,
		instance,
		cursor,
		className,
	); err != nil {
		fmt.Println("[Party Picker] RegisterClassEx failed:", err)
		return
	}

	title, _ :=
		windows.UTF16PtrFromString(
			"KaTools Party ROI Picker",
		)

	hwnd, _, errCode :=
		createWindowEx.Call(
			uintptr(
				wsExLayered|
					wsExToolWindow|
					wsExTopmost,
			),

			uintptr(
				unsafe.Pointer(className),
			),

			uintptr(
				unsafe.Pointer(title),
			),

			uintptr(wsPopup),

			uintptr(state.targetRect.Left),
			uintptr(state.targetRect.Top),

			uintptr(
				state.targetRect.Right-
					state.targetRect.Left,
			),

			uintptr(
				state.targetRect.Bottom-
					state.targetRect.Top,
			),

			0,
			0,
			instance,
			uintptr(unsafe.Pointer(state)),
		)

	if hwnd == 0 {
		fmt.Println(
			"[Party Picker] CreateWindowEx failed:",
			errCode,
		)
		return
	}

	state.mu.Lock()
	state.hwnd = windows.Handle(hwnd)
	state.mu.Unlock()

	setLayeredWindowAttributes(
		hwnd,
		0,
		80,
		lwaAlpha,
	)

	setWindowPos.Call(
		hwnd,
		uintptr(^uintptr(0)),
		uintptr(state.targetRect.Left),
		uintptr(state.targetRect.Top),
		uintptr(
			state.targetRect.Right-
				state.targetRect.Left,
		),
		uintptr(
			state.targetRect.Bottom-
				state.targetRect.Top,
		),
		uintptr(
			swpNoActivate|
				swpShowWindow,
		),
	)

	showWindow.Call(
		hwnd,
		swShow,
	)

	updateWindow.Call(hwnd)

	fmt.Println()
	fmt.Println("----------------------------------------")
	fmt.Println("PARTY OCR ROI PICKER")
	fmt.Println("----------------------------------------")
	fmt.Println(
		"Drag over the party popup text.",
	)
	fmt.Println(
		"ESC = cancel",
	)
	fmt.Println("----------------------------------------")

	var msg pickerMSG

	for {
		ret, _, _ :=
			getMessage.Call(
				uintptr(unsafe.Pointer(&msg)),
				0,
				0,
				0,
			)

		if int32(ret) <= 0 {
			break
		}

		translateMessage.Call(
			uintptr(unsafe.Pointer(&msg)),
		)

		dispatchMessage.Call(
			uintptr(unsafe.Pointer(&msg)),
		)
	}

	time.Sleep(30 * time.Millisecond)
}

func ensurePartyPickerClass(
	registerClassEx *windows.LazyProc,
	instance uintptr,
	cursor uintptr,
	className *uint16,
) error {
	partyPickerClassOnce.Do(func() {
		wc := pickerWNDCLASSEX{
			CbSize:        uint32(unsafe.Sizeof(pickerWNDCLASSEX{})),
			LpfnWndProc:   partyPickerWndProcCallback,
			HInstance:     instance,
			HCursor:       cursor,
			LpszClassName: className,
		}

		registered, _, callErr := registerClassEx.Call(
			uintptr(unsafe.Pointer(&wc)),
		)
		if registered == 0 && callErr != syscall.Errno(1410) { // ERROR_CLASS_ALREADY_EXISTS
			partyPickerClassErr = callErr
		}
	})

	return partyPickerClassErr
}

func partyPickerWndProc(
	hwnd uintptr,
	msg uint32,
	wParam uintptr,
	lParam uintptr,
) (result uintptr) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// Panics escaping a Windows callback terminate the whole process.
			// Keep the picker failure isolated and let the user retry selection.
			fmt.Println("[ROI Picker] Window callback recovered:", recovered)
			result = 0
		}
	}()

	state := getPickerState(hwnd)

	switch msg {

	case wmLButtonDown:

		if state == nil {
			break
		}

		x, y, ok := pickerCursorToTargetPoint(state)
		if !ok {
			return 0
		}

		state.mu.Lock()

		state.dragging = true

		state.startX = x
		state.startY = y

		state.currentX = x
		state.currentY = y

		state.mu.Unlock()

		setMouseCapture(hwnd)
		invalidatePicker(hwnd)

		return 0

	case wmMouseMove:

		if state == nil {
			break
		}

		state.mu.Lock()

		if state.dragging {
			if x, y, ok := pickerCursorToTargetPoint(state); ok {
				state.currentX = x
				state.currentY = y
			}
		}

		state.mu.Unlock()

		invalidatePicker(hwnd)

		return 0

	case wmLButtonUp:

		if state == nil {
			break
		}

		state.mu.Lock()

		if state.dragging {
			if x, y, ok := pickerCursorToTargetPoint(state); ok {
				state.currentX = x
				state.currentY = y
			}

			state.dragging = false

			x1 := state.startX
			y1 := state.startY
			x2 := state.currentX
			y2 := state.currentY

			state.mu.Unlock()

			releaseMouseCapture()

			roi, saved := savePickedROI(
				state,
				x1,
				y1,
				x2,
				y2,
			)

			if saved {
				state.mu.Lock()
				state.captureEvidence = true
				state.selectedROI = roi
				state.mu.Unlock()
			}

			destroyPickerWindow(hwnd)

			return 0
		}

		state.mu.Unlock()

		releaseMouseCapture()

		return 0

	case wmRButtonDown:

		destroyPickerWindow(hwnd)

		return 0

	case wmKeyDown:

		if wParam == vkEscape {
			destroyPickerWindow(hwnd)
			return 0
		}

	case wmPaint:
		// CreateWindowEx may synchronously send WM_PAINT before state.hwnd is
		// assigned. Let Windows handle that initial paint instead of dereferencing
		// a nil picker state (which previously terminated the app on a reselect).
		if state == nil {
			return defaultPickerWindowProc(
				hwnd,
				msg,
				wParam,
				lParam,
			)
		}

		paintPartyPicker(
			hwnd,
			state,
		)

		return 0

	case wmDestroy:

		postPickerQuitMessage(0)

		return 0
	}

	return defaultPickerWindowProc(
		hwnd,
		msg,
		wParam,
		lParam,
	)
}

// capturePickerEvidence runs from runPartyROIPicker's teardown path.  At that
// point the overlay is gone but the game is still foreground, so the desktop
// preview cannot accidentally contain the KaTools dashboard.
func capturePickerEvidence(state *partyPickerState) {
	if state == nil {
		return
	}

	state.mu.Lock()
	capture := state.captureEvidence
	selected := state.selectedROI
	targetRect := state.targetRect
	kind := state.kind
	state.mu.Unlock()
	if !capture {
		return
	}

	// Let DWM present the destroyed overlay before taking the desktop preview.
	time.Sleep(80 * time.Millisecond)

	var err error
	if kind == "status" {
		err = captureStatusROIPreview(targetRect, selected)
	} else if kind == "target" {
		err = captureTargetROIPreview(targetRect, selected)
	} else if kind == "death" {
		err = captureDeathROIPreview(targetRect, selected)
	} else {
		err = capturePartyROIPreview(targetRect, selected)
	}
	if err != nil {
		fmt.Println("[ROI Picker] Preview capture failed:", err)
	}
	if globalRuntimeManager != nil {
		if err := globalRuntimeManager.CaptureWGCROISnapshot(kind, selected); err != nil {
			fmt.Println("[WGC Snapshot]", err)
		}
	}
}

func getPickerState(
	hwnd uintptr,
) *partyPickerState {

	partyPickerMu.Lock()
	state := activePicker
	partyPickerMu.Unlock()

	if state == nil {
		return nil
	}

	state.mu.Lock()
	matches := uintptr(state.hwnd) == hwnd
	state.mu.Unlock()
	if !matches {
		return nil
	}

	return state
}

func paintPartyPicker(
	hwnd uintptr,
	state *partyPickerState,
) {

	user32 := windows.NewLazySystemDLL("user32.dll")

	beginPaint := user32.NewProc(
		"BeginPaint",
	)

	endPaint := user32.NewProc(
		"EndPaint",
	)

	var ps pickerPAINTSTRUCT

	hdc, _, _ :=
		beginPaint.Call(
			hwnd,
			uintptr(unsafe.Pointer(&ps)),
		)

	if hdc == 0 {
		return
	}

	state.mu.Lock()

	x1 := state.startX
	y1 := state.startY

	x2 := state.currentX
	y2 := state.currentY

	dragging := state.dragging

	state.mu.Unlock()

	if dragging {

		left := x1
		right := x2

		if right < left {
			left, right = right, left
		}

		top := y1
		bottom := y2

		if bottom < top {
			top, bottom = bottom, top
		}

		drawPickerRectangle(
			hdc,
			left,
			top,
			right,
			bottom,
		)
	}

	endPaint.Call(
		hwnd,
		uintptr(unsafe.Pointer(&ps)),
	)
}

func drawPickerRectangle(
	hdc uintptr,
	left int,
	top int,
	right int,
	bottom int,
) {

	gdi32 := windows.NewLazySystemDLL(
		"gdi32.dll",
	)

	createPen := gdi32.NewProc(
		"CreatePen",
	)

	selectObject := gdi32.NewProc(
		"SelectObject",
	)

	moveToEx := gdi32.NewProc(
		"MoveToEx",
	)

	lineTo := gdi32.NewProc(
		"LineTo",
	)

	deleteObject := gdi32.NewProc(
		"DeleteObject",
	)

	pen, _, _ :=
		createPen.Call(
			psSolid,
			3,
			colorRed,
		)

	oldPen, _, _ :=
		selectObject.Call(
			hdc,
			pen,
		)

	moveToEx.Call(
		hdc,
		uintptr(left),
		uintptr(top),
		0,
	)

	lineTo.Call(
		hdc,
		uintptr(right),
		uintptr(top),
	)

	lineTo.Call(
		hdc,
		uintptr(right),
		uintptr(bottom),
	)

	lineTo.Call(
		hdc,
		uintptr(left),
		uintptr(bottom),
	)

	lineTo.Call(
		hdc,
		uintptr(left),
		uintptr(top),
	)

	selectObject.Call(
		hdc,
		oldPen,
	)

	deleteObject.Call(pen)
}

func savePickedROI(
	state *partyPickerState,
	x1 int,
	y1 int,
	x2 int,
	y2 int,
) (ocrworker.PartyROIConfig, bool) {

	left := x1
	right := x2

	if right < left {
		left, right = right, left
	}

	top := y1
	bottom := y2

	if bottom < top {
		top, bottom = bottom, top
	}

	width := right - left
	height := bottom - top

	if width < 5 || height < 5 {

		fmt.Println(
			"[Party Picker] Selection too small.",
		)

		return ocrworker.PartyROIConfig{}, false
	}

	roi := partyPickerSelectionToROI(
		state.targetRect,
		left,
		top,
		width,
		height,
	)
	var err error
	if state.kind == "status" {
		err = SavePickedStatusROI(roi)
	} else if state.kind == "target" {
		err = SavePickedTargetROI(roi)
	} else if state.kind == "death" {
		err = SavePickedDeathROI(roi)
	} else {
		err = ocrworker.SavePickedPartyROI(roi)
	}
	if err != nil {

		fmt.Println(
			"[Party Picker] Save failed:",
			err,
		)

		return ocrworker.PartyROIConfig{}, false
	}
	_ = hideROIConfigFiles()

	fmt.Println()
	fmt.Println("----------------------------------------")
	fmt.Println("PARTY OCR ROI SAVED")
	fmt.Println("----------------------------------------")

	fmt.Printf(
		"ROI: X=%d Y=%d W=%d H=%d\n",
		roi.X,
		roi.Y,
		roi.Width,
		roi.Height,
	)

	fmt.Println("----------------------------------------")

	return roi, true
}

// partyPickerSelectionToROI converts a picker drag into coordinates relative
// to the target window. WM_MOUSE messages normally already use client
// coordinates. Some windowed games, however, report screen coordinates to the
// transparent popup overlay; detect that form before saving it. Without this
// conversion, an ROI such as the target window's desktop position is used as
// an in-frame position and OCR reads an unrelated area.
func partyPickerSelectionToROI(
	target pickerRECT,
	left int,
	top int,
	width int,
	height int,
) ocrworker.PartyROIConfig {
	targetWidth := int(target.Right - target.Left)
	targetHeight := int(target.Bottom - target.Top)

	// Keep the persisted selection inside the target window even when a drag
	// ends a few pixels outside the overlay.
	if left < 0 {
		width += left
		left = 0
	}
	if top < 0 {
		height += top
		top = 0
	}
	if left+width > targetWidth {
		width = targetWidth - left
	}
	if top+height > targetHeight {
		height = targetHeight - top
	}

	return ocrworker.PartyROIConfig{
		X:      left,
		Y:      top,
		Width:  width,
		Height: height,
	}
}

// pickerCursorToTargetPoint deliberately uses GetCursorPos instead of the
// mouse-message lParam. A few games supply desktop coordinates in lParam for
// the transparent picker popup, while GetCursorPos is always a desktop point.
// Converting it here gives one unambiguous, target-relative coordinate system.
func pickerCursorToTargetPoint(state *partyPickerState) (int, int, bool) {
	if state == nil {
		return 0, 0, false
	}

	user32 := windows.NewLazySystemDLL("user32.dll")
	getCursorPos := user32.NewProc("GetCursorPos")

	var point pickerPOINT
	ret, _, _ := getCursorPos.Call(uintptr(unsafe.Pointer(&point)))
	if ret == 0 {
		return 0, 0, false
	}

	return int(point.X - state.targetRect.Left),
		int(point.Y - state.targetRect.Top), true
}

func getPickerWindowRect(
	hwnd uintptr,
) (pickerRECT, error) {

	user32 := windows.NewLazySystemDLL(
		"user32.dll",
	)

	getClientRect := user32.NewProc("GetClientRect")
	clientToScreen := user32.NewProc("ClientToScreen")

	var client pickerRECT
	if ret, _, err := getClientRect.Call(
		hwnd, uintptr(unsafe.Pointer(&client)),
	); ret == 0 {
		return pickerRECT{}, fmt.Errorf("GetClientRect failed: %v", err)
	}

	topLeft := pickerPOINT{X: client.Left, Y: client.Top}
	bottomRight := pickerPOINT{X: client.Right, Y: client.Bottom}
	if ret, _, err := clientToScreen.Call(
		hwnd, uintptr(unsafe.Pointer(&topLeft)),
	); ret == 0 {
		return pickerRECT{}, fmt.Errorf("ClientToScreen (top left) failed: %v", err)
	}
	if ret, _, err := clientToScreen.Call(
		hwnd, uintptr(unsafe.Pointer(&bottomRight)),
	); ret == 0 {
		return pickerRECT{}, fmt.Errorf("ClientToScreen (bottom right) failed: %v", err)
	}

	return pickerRECT{
		Left:   topLeft.X,
		Top:    topLeft.Y,
		Right:  bottomRight.X,
		Bottom: bottomRight.Y,
	}, nil
}

func loadPickerCursor() uintptr {

	user32 := windows.NewLazySystemDLL(
		"user32.dll",
	)

	loadCursor := user32.NewProc(
		"LoadCursorW",
	)

	// IDC_CROSS = 32515
	cursor, _, _ :=
		loadCursor.Call(
			0,
			uintptr(32515),
		)

	return cursor
}

func setLayeredWindowAttributes(
	hwnd uintptr,
	colorKey uint32,
	alpha byte,
	flags uint32,
) {

	user32 := windows.NewLazySystemDLL(
		"user32.dll",
	)

	proc := user32.NewProc(
		"SetLayeredWindowAttributes",
	)

	proc.Call(
		hwnd,
		uintptr(colorKey),
		uintptr(alpha),
		uintptr(flags),
	)
}

func setMouseCapture(hwnd uintptr) {

	user32 := windows.NewLazySystemDLL(
		"user32.dll",
	)

	proc := user32.NewProc(
		"SetCapture",
	)

	proc.Call(hwnd)
}

func releaseMouseCapture() {

	user32 := windows.NewLazySystemDLL(
		"user32.dll",
	)

	proc := user32.NewProc(
		"ReleaseCapture",
	)

	proc.Call()
}

func invalidatePicker(hwnd uintptr) {

	user32 := windows.NewLazySystemDLL(
		"user32.dll",
	)

	proc := user32.NewProc(
		"InvalidateRect",
	)

	proc.Call(
		hwnd,
		0,
		1,
	)
}

func destroyPickerWindow(hwnd uintptr) {

	user32 := windows.NewLazySystemDLL(
		"user32.dll",
	)

	proc := user32.NewProc(
		"DestroyWindow",
	)

	proc.Call(hwnd)
}

func postPickerQuitMessage(code int) {

	user32 := windows.NewLazySystemDLL(
		"user32.dll",
	)

	proc := user32.NewProc(
		"PostQuitMessage",
	)

	proc.Call(uintptr(code))
}

func defaultPickerWindowProc(
	hwnd uintptr,
	msg uint32,
	wParam uintptr,
	lParam uintptr,
) uintptr {

	user32 := windows.NewLazySystemDLL(
		"user32.dll",
	)

	proc := user32.NewProc(
		"DefWindowProcW",
	)

	ret, _, _ :=
		proc.Call(
			hwnd,
			uintptr(msg),
			wParam,
			lParam,
		)

	return ret
}
