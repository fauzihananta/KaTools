package main

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ============================================================
// Native Win32 Bot UI
// No github.com/lxn/walk
// ============================================================

const (
	uiClassName = "KaToolsBotWindow"

	wsOverlappedWindow = 0x00CF0000
	wsChild            = 0x40000000
	wsVisible          = 0x10000000
	wsBorder           = 0x00800000
	wsTabStop          = 0x00010000

	bsAutoCheckBox = 0x00000003
	bsPushButton   = 0x00000000

	esLeft   = 0x0000
	esNumber = 0x2000

	wmDestroy = 0x0002
	wmCommand = 0x0111
	wmClose   = 0x0010

	bmGetCheck = 0x00F0

	bnClicked = 0

	bstUnchecked = 0
	bstChecked   = 1

	uiIDStart = 1000

	uiIDTargetCheck = 1100
	uiIDTargetDelay = 1101

	uiIDAttackCheck = 1110
	uiIDAttackDelay = 1111

	uiIDPickCheck = 1120
	uiIDPickDelay = 1121

	uiIDSkillBase = 2000
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procUnregisterClassW = user32.NewProc("UnregisterClassW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procGetWindowTextW   = user32.NewProc("GetWindowTextW")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

type nativeUIState struct {
	hwnd uintptr

	defaultConfig BotConfig

	controls map[int]uintptr

	result    *BotConfig
	resultErr error

	done bool
}

var activeBotUI *nativeUIState

type point struct {
	X int32
	Y int32
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  uintptr
	LpszClassName uintptr
	HIconSm       uintptr
}

func utf16Ptr(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

func loword(v uintptr) uint16 {
	return uint16(v & 0xFFFF)
}

func hiword(v uintptr) uint16 {
	return uint16((v >> 16) & 0xFFFF)
}

func getWindowText(hwnd uintptr) string {
	var buffer [128]uint16

	ret, _, _ := procGetWindowTextW.Call(
		hwnd,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
	)

	if ret == 0 {
		return ""
	}

	return syscall.UTF16ToString(buffer[:ret])
}

func setWindowText(hwnd uintptr, text string) {
	procSendMessageW.Call(
		hwnd,
		0x000C,
		0,
		uintptr(unsafe.Pointer(utf16Ptr(text))),
	)
}

func createControl(
	parent uintptr,
	className string,
	text string,
	style uintptr,
	exStyle uintptr,
	x, y, width, height int,
	id int,
) uintptr {

	hwnd, _, _ := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(utf16Ptr(className))),
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		style,
		uintptr(x),
		uintptr(y),
		uintptr(width),
		uintptr(height),
		parent,
		uintptr(id),
		getModuleHandle(),
		0,
	)

	return hwnd
}

func getModuleHandle() uintptr {
	ret, _, _ := procGetModuleHandleW.Call(0)
	return ret
}

func isChecked(hwnd uintptr) bool {
	ret, _, _ := procSendMessageW.Call(
		hwnd,
		bmGetCheck,
		0,
		0,
	)

	return ret == bstChecked
}

var botUIWndProc = syscall.NewCallback(
	func(
		hwnd uintptr,
		message uint32,
		wParam uintptr,
		lParam uintptr,
	) uintptr {

		switch message {

		case wmCommand:

			id := int(loword(wParam))
			notify := hiword(wParam)

			fmt.Printf(
				"[UI] WM_COMMAND id=%d notify=%d\n",
				id,
				notify,
			)

			if id == uiIDStart {

				fmt.Println("[UI] START BOT clicked")

				if activeBotUI != nil {
					activeBotUI.handleStart()
				}

				return 0
			}

		case wmClose:

			if activeBotUI != nil {
				activeBotUI.done = true
			}

			procDestroyWindow.Call(hwnd)

			return 0

		case wmDestroy:

			procPostQuitMessage.Call(0)

			return 0
		}

		ret, _, _ := procDefWindowProcW.Call(
			hwnd,
			uintptr(message),
			wParam,
			lParam,
		)

		return ret
	},
)

func buildBotUI(
	hwnd uintptr,
	defaultConfig BotConfig,
) (*BotConfig, error) {

	_ = hwnd

	state := &nativeUIState{
		defaultConfig: defaultConfig,
		controls:      make(map[int]uintptr),
	}

	activeBotUI = state

	defer func() {
		activeBotUI = nil
	}()

	instance := getModuleHandle()

	className := utf16Ptr(uiClassName)

	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   botUIWndProc,
		HInstance:     instance,
		HbrBackground: 6,
		LpszClassName: uintptr(unsafe.Pointer(className)),
	}

	loadCursor := user32.NewProc("LoadCursorW")

	cursor, _, _ := loadCursor.Call(
		0,
		32512,
	)

	wc.HCursor = cursor

	ret, _, err := procRegisterClassExW.Call(
		uintptr(unsafe.Pointer(&wc)),
	)

	if ret == 0 {

		if err != syscall.Errno(1410) {

			return nil, fmt.Errorf(
				"RegisterClassExW failed: %v",
				err,
			)
		}
	}

	defer procUnregisterClassW.Call(
		uintptr(unsafe.Pointer(className)),
		instance,
	)

	mainHWND, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr("KaTools Kathana Bot"))),
		wsOverlappedWindow,
		100,
		100,
		620,
		760,
		0,
		0,
		instance,
		0,
	)

	if mainHWND == 0 {

		return nil, fmt.Errorf(
			"CreateWindowExW failed: %v",
			err,
		)
	}

	state.hwnd = mainHWND

	createControl(
		mainHWND,
		"STATIC",
		"KaTools Kathana Bot",
		wsChild|wsVisible,
		0,
		20,
		15,
		300,
		30,
		0,
	)

	createControl(
		mainHWND,
		"STATIC",
		"Actions",
		wsChild|wsVisible,
		0,
		20,
		55,
		300,
		25,
		0,
	)

	state.addAction(
		mainHWND,
		"Target (E)",
		uiIDTargetCheck,
		uiIDTargetDelay,
		defaultConfig.TargetEnabled,
		defaultConfig.TargetDelay,
		90,
	)

	state.addAction(
		mainHWND,
		"Attack (R)",
		uiIDAttackCheck,
		uiIDAttackDelay,
		defaultConfig.AttackEnabled,
		defaultConfig.AttackDelay,
		125,
	)

	state.addAction(
		mainHWND,
		"Pick (F)",
		uiIDPickCheck,
		uiIDPickDelay,
		defaultConfig.PickEnabled,
		defaultConfig.PickDelay,
		160,
	)

	createControl(
		mainHWND,
		"STATIC",
		"Skills",
		wsChild|wsVisible,
		0,
		20,
		205,
		300,
		25,
		0,
	)

	createControl(
		mainHWND,
		"STATIC",
		"Skill",
		wsChild|wsVisible,
		0,
		25,
		235,
		80,
		25,
		0,
	)

	createControl(
		mainHWND,
		"STATIC",
		"Enabled",
		wsChild|wsVisible,
		0,
		115,
		235,
		80,
		25,
		0,
	)

	createControl(
		mainHWND,
		"STATIC",
		"Delay (sec)",
		wsChild|wsVisible,
		0,
		215,
		235,
		100,
		25,
		0,
	)

	for i, skill := range defaultConfig.Skills {

		y := 265 + (i * 24)

		checkID := uiIDSkillBase + (i * 2)
		delayID := uiIDSkillBase + (i * 2) + 1

		createControl(
			mainHWND,
			"STATIC",
			skill.Name,
			wsChild|wsVisible,
			0,
			25,
			y,
			80,
			22,
			0,
		)

		check := createControl(
			mainHWND,
			"BUTTON",
			"",
			wsChild|wsVisible|wsTabStop|bsAutoCheckBox,
			0,
			140,
			y,
			20,
			20,
			checkID,
		)

		if skill.Enabled {

			procSendMessageW.Call(
				check,
				0x00F1,
				bstChecked,
				0,
			)
		}

		delay := createControl(
			mainHWND,
			"EDIT",
			fmt.Sprintf(
				"%.0f",
				skill.Delay.Seconds(),
			),
			wsChild|wsVisible|wsBorder|wsTabStop|esLeft|esNumber,
			0x00000200,
			215,
			y,
			100,
			22,
			delayID,
		)

		state.controls[checkID] = check
		state.controls[delayID] = delay
	}

	createControl(
		mainHWND,
		"BUTTON",
		"START BOT",
		wsChild|wsVisible|wsTabStop|bsPushButton,
		0,
		220,
		650,
		160,
		40,
		uiIDStart,
	)

	procShowWindow.Call(
		mainHWND,
		1,
	)

	procUpdateWindow.Call(
		mainHWND,
	)

	var message msg

	for {

		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&message)),
			0,
			0,
			0,
		)

		if int32(ret) <= 0 {
			break
		}

		procTranslateMessage.Call(
			uintptr(unsafe.Pointer(&message)),
		)

		procDispatchMessageW.Call(
			uintptr(unsafe.Pointer(&message)),
		)
	}

	if state.result == nil {

		if state.resultErr != nil {
			return nil, state.resultErr
		}

		return nil, fmt.Errorf(
			"UI closed without starting bot",
		)
	}

	return state.result, nil
}

func (s *nativeUIState) addAction(
	parent uintptr,
	name string,
	checkID int,
	delayID int,
	enabled bool,
	delay time.Duration,
	y int,
) {

	createControl(
		parent,
		"STATIC",
		name,
		wsChild|wsVisible,
		0,
		25,
		y,
		100,
		22,
		0,
	)

	check := createControl(
		parent,
		"BUTTON",
		"",
		wsChild|wsVisible|wsTabStop|bsAutoCheckBox,
		0,
		140,
		y,
		20,
		20,
		checkID,
	)

	if enabled {

		procSendMessageW.Call(
			check,
			0x00F1,
			bstChecked,
			0,
		)
	}

	delayEdit := createControl(
		parent,
		"EDIT",
		fmt.Sprintf(
			"%.0f",
			delay.Seconds(),
		),
		wsChild|wsVisible|wsBorder|wsTabStop|esLeft|esNumber,
		0x00000200,
		215,
		y,
		100,
		22,
		delayID,
	)

	s.controls[checkID] = check
	s.controls[delayID] = delayEdit
}

func (s *nativeUIState) handleStart() {

	config := s.defaultConfig

	targetDelay, err := parseDelay(
		getWindowText(
			s.controls[uiIDTargetDelay],
		),
	)

	if err != nil {

		s.showError(
			"Target delay harus berupa angka yang lebih besar dari 0.",
		)

		return
	}

	config.TargetEnabled =
		isChecked(
			s.controls[uiIDTargetCheck],
		)

	config.TargetDelay =
		targetDelay

	attackDelay, err := parseDelay(
		getWindowText(
			s.controls[uiIDAttackDelay],
		),
	)

	if err != nil {

		s.showError(
			"Attack delay harus berupa angka yang lebih besar dari 0.",
		)

		return
	}

	config.AttackEnabled =
		isChecked(
			s.controls[uiIDAttackCheck],
		)

	config.AttackDelay =
		attackDelay

	pickDelay, err := parseDelay(
		getWindowText(
			s.controls[uiIDPickDelay],
		),
	)

	if err != nil {

		s.showError(
			"Pick delay harus berupa angka yang lebih besar dari 0.",
		)

		return
	}

	config.PickEnabled =
		isChecked(
			s.controls[uiIDPickCheck],
		)

	config.PickDelay =
		pickDelay

	for i := range config.Skills {

		checkID :=
			uiIDSkillBase +
				(i * 2)

		delayID :=
			uiIDSkillBase +
				(i * 2) +
				1

		config.Skills[i].Enabled =
			isChecked(
				s.controls[checkID],
			)

		delayValue, err :=
			parseDelay(
				getWindowText(
					s.controls[delayID],
				),
			)

		if err != nil {

			s.showError(
				fmt.Sprintf(
					"Delay skill %s harus berupa angka yang lebih besar dari 0.",
					config.Skills[i].Name,
				),
			)

			return
		}

		config.Skills[i].Delay =
			delayValue
	}

	config.Enabled = true
	fmt.Println("[UI] Config validated successfully")
	s.result = &config

	procDestroyWindow.Call(
		s.hwnd,
	)
}

func (s *nativeUIState) showError(text string) {

	messageBox := user32.NewProc(
		"MessageBoxW",
	)

	messageBox.Call(
		s.hwnd,
		uintptr(
			unsafe.Pointer(
				utf16Ptr(text),
			),
		),
		uintptr(
			unsafe.Pointer(
				utf16Ptr("Invalid Input"),
			),
		),
		0x00000010,
	)
}

func parseDelay(
	value string,
) (time.Duration, error) {

	var seconds float64

	_, err := fmt.Sscanf(
		value,
		"%f",
		&seconds,
	)

	if err != nil {
		return 0, err
	}

	if seconds <= 0 {

		return 0, fmt.Errorf(
			"delay must be greater than zero",
		)
	}

	return time.Duration(
		seconds *
			float64(time.Second),
	), nil
}
