package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"KaTools/tools/ocrworker"

	"golang.org/x/sys/windows"
)

const (
	sharedMemoryName = `Local\KaToolsKathanaFrameV2`

	magic      = 0x4B544652
	headerSize = 40

	VK_F8 = 0x77
	VK_F9 = 0x78

	// ========================================================
	// Party Detector ROI
	// ========================================================
	//
	// WGC frame terakhir:
	// 1536 x 864
	//
	// Popup berada kurang lebih di:
	// X=553..853
	// Y=472..583
	//
	// Detector memakai ROI yang lebih luas untuk menangkap
	// perubahan visual popup.
	//
	partyROI_X      = 750
	partyROI_Y      = 650
	partyROI_Width  = 400
	partyROI_Height = 150

	// Sample detector sekitar 8x per second.
	partyDetectorInterval = 120 * time.Millisecond

	// Baseline adaptasi lambat.
	partyBaselineAlpha = 0.03

	// Hysteresis.
	partyDiffThreshold  = 18.0
	partyClearThreshold = 10.0

	// Require beberapa frame berturut-turut.
	partyConfirmFrames = 3

	// Warmup baseline.
	partyWarmupFrames = 12

	// Delay sebelum ENTER.
	partyAcceptDelay = 120 * time.Millisecond

	// Cooldown ENTER.
	partyEnterCooldown = 800 * time.Millisecond

	// Targeted keyboard.
	vkReturn = 0x0D
)

type SharedFrameHeader struct {
	Magic         uint32
	Width         uint32
	Height        uint32
	Stride        uint32
	BytesPerPixel uint32
	FrameNumber   uint64
	DataSize      uint32
	FrameReady    int32
}

type Pixel struct {
	R byte
	G byte
	B byte
	A byte
}

type FrameReader struct {
	memory uintptr
	pixels []byte

	width  int
	height int
	stride int
}

type POINT struct {
	X int32
	Y int32
}

type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type GUIThreadInfo struct {
	CbSize        uint32
	Flags         uint32
	HwndActive    uintptr
	HwndFocus     uintptr
	HwndCapture   uintptr
	HwndMenuOwner uintptr
	HwndMoveSize  uintptr
	HwndCaret     uintptr
	RcCaret       RECT
}

// ============================================================
// MAIN
// ============================================================
var globalRuntimeManager *RuntimeManager

func main() {
	// Picker overlay, GetWindowRect, dan BitBlt harus memakai koordinat fisik
	// yang sama. Tanpa ini Windows dapat memvirtualisasi koordinat pada monitor
	// dengan display scaling (125%/150%), sehingga preview bergeser ke kiri atas.
	enablePerMonitorDPIAwareness()

	fmt.Println("========================================")
	fmt.Println(" KaTools")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("Web UI will start first.")
	fmt.Println("Select the target window from the UI,")
	fmt.Println("then press START.")
	fmt.Println()

	runtimeManager := NewRuntimeManager()
	globalRuntimeManager = runtimeManager
	setupShutdown(runtimeManager)
	// Existing saved areas are implementation details, not regular user files.
	// New areas are hidden again immediately after the picker saves them.
	_ = hideROIConfigFiles()

	if err := startWebView2UI(runtimeManager); err != nil {
		fmt.Println("KaTools UI error:", err)
	}

	// The native WebView2 window returns only after it has closed. Keep the
	// existing runtime cleanup path for a normal close.
	runtimeManager.Stop()
}

func enablePerMonitorDPIAwareness() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("SetProcessDpiAwarenessContext")

	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4.
	// Windows lama tidak menyediakan API ini; dalam kasus itu aplikasi tetap
	// berjalan dengan perilaku sebelumnya.
	_, _, _ = proc.Call(uintptr(^uintptr(3)))
}

// ============================================================
// Runtime Manager
// ============================================================

type RuntimeManager struct {
	mu sync.RWMutex

	hwnd uintptr

	wgcCmd *exec.Cmd

	mapping windows.Handle
	memory  uintptr
	reader  *FrameReader

	bot         *BotController
	autoAccept  *AutoAcceptController
	autoPot     *AutoPotController
	emergency   *EmergencySkillController
	deathPause  *DeathPauseController
	targetUntil *TargetUntilDeadController

	rect           RECT
	scaleX         float64
	scaleY         float64
	frameOriginX   int
	frameOriginY   int
	captureOffsetX int
	captureOffsetY int

	running  bool
	starting bool

	stopCh chan struct{}
	doneCh chan struct{}
}

func NewRuntimeManager() *RuntimeManager {
	return &RuntimeManager{}
}

// ============================================================
// START
// ============================================================

func (m *RuntimeManager) Start(
	hwnd uintptr,
	cfg WebBotConfig,
) error {

	m.mu.Lock()

	if m.running || m.starting {
		m.mu.Unlock()

		return fmt.Errorf(
			"KaTools is already running or starting",
		)
	}

	m.starting = true

	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.starting = false
		m.mu.Unlock()
	}()

	if hwnd == 0 {
		return fmt.Errorf("invalid window handle")
	}

	if !isWindowValid(hwnd) {
		return fmt.Errorf(
			"selected window no longer exists",
		)
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("Starting KaTools Runtime")
	fmt.Println("========================================")

	// The normal bot does not need screen capture. WGC, frame mapping, and OCR
	// are required only by Auto Accept Party or Auto Potion.
	bot := NewBotController(hwnd, newDefaultBotConfig())
	autoAccept := NewAutoAcceptController(cfg.AutoAcceptEnabled)
	autoPot := NewAutoPotController(hwnd, cfg)
	emergency := NewEmergencySkillController(hwnd, cfg)
	deathPause := NewDeathPauseController(cfg.AutoPauseDeathEnabled, cfg.AutoResurrectEnabled)
	targetFilterEnabled := cfg.TargetUntilDeadEnabled ||
		(cfg.TargetEnabled && strings.TrimSpace(cfg.TargetUntilDeadCharacterName) != "")
	targetUntil := NewTargetUntilDeadController(
		cfg.TargetUntilDeadEnabled,
		targetFilterEnabled,
		cfg.TargetUntilDeadEnabled && cfg.TargetUntilDeadSupport,
		cfg.TargetUntilDeadCharacterName,
	)
	applyWebBotConfig(bot, autoAccept, autoPot, emergency, deathPause, targetUntil, cfg)

	if !cfg.AutoAcceptEnabled && !autoPot.IsAnyEnabled() && !emergency.IsAnyEnabled() && !deathPause.IsEnabled() && !targetUntil.IsFilterEnabled() {
		m.mu.Lock()
		m.hwnd = hwnd
		m.bot = bot
		m.autoAccept = autoAccept
		m.autoPot = autoPot
		m.emergency = emergency
		m.deathPause = deathPause
		m.targetUntil = targetUntil
		m.running = true
		m.mu.Unlock()

		fmt.Println("[WGC] Disabled: Auto Accept Party is unchecked.")
		bot.Start()
		return nil
	}

	// ========================================================
	// Start WGC (Auto Accept Party only)
	// ========================================================

	wgcCmd, err := startWGCCapture(hwnd)

	if err != nil {
		return err
	}

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	m.mu.Lock()

	m.wgcCmd = wgcCmd
	m.hwnd = hwnd
	m.stopCh = stopCh
	m.doneCh = doneCh

	m.mu.Unlock()

	// ========================================================
	// Wait Shared Memory
	// ========================================================

	mapping, err :=
		waitForSharedMemoryWithStop(
			stopCh,
			15*time.Second,
		)

	if err != nil {

		if wgcCmd.Process != nil {
			_ = wgcCmd.Process.Kill()
		}

		return fmt.Errorf(
			"waiting for WGC shared memory failed: %w",
			err,
		)
	}

	// ========================================================
	// Map Shared Memory
	// ========================================================

	memory, err :=
		windows.MapViewOfFile(
			mapping,
			windows.FILE_MAP_READ,
			0,
			0,
			0,
		)

	if err != nil {

		_ = windows.CloseHandle(mapping)

		if wgcCmd.Process != nil {
			_ = wgcCmd.Process.Kill()
		}

		return fmt.Errorf(
			"MapViewOfFile failed: %w",
			err,
		)
	}

	base := uintptr(memory)

	header := readHeader(base)

	if header.Magic != magic {

		_ = windows.UnmapViewOfFile(base)
		_ = windows.CloseHandle(mapping)

		if wgcCmd.Process != nil {
			_ = wgcCmd.Process.Kill()
		}

		return fmt.Errorf(
			"invalid shared memory magic: 0x%X",
			header.Magic,
		)
	}

	if header.Width == 0 ||
		header.Height == 0 ||
		header.Stride == 0 ||
		header.DataSize == 0 {

		_ = windows.UnmapViewOfFile(base)
		_ = windows.CloseHandle(mapping)

		if wgcCmd.Process != nil {
			_ = wgcCmd.Process.Kill()
		}

		return fmt.Errorf(
			"invalid WGC frame header",
		)
	}

	reader := &FrameReader{
		memory: base,
		width:  int(header.Width),
		height: int(header.Height),
		stride: int(header.Stride),
	}

	reader.pixels = unsafe.Slice(
		(*byte)(unsafe.Pointer(
			base+headerSize,
		)),
		int(header.DataSize),
	)

	// ========================================================
	// Window Geometry
	// ========================================================

	rect, err :=
		getWindowRect(
			windows.Handle(hwnd),
		)

	if err != nil {

		_ = windows.UnmapViewOfFile(base)
		_ = windows.CloseHandle(mapping)

		if wgcCmd.Process != nil {
			_ = wgcCmd.Process.Kill()
		}

		return fmt.Errorf(
			"GetWindowRect failed: %w",
			err,
		)
	}

	windowWidth :=
		int(rect.Right - rect.Left)

	windowHeight :=
		int(rect.Bottom - rect.Top)

	if windowWidth <= 0 ||
		windowHeight <= 0 {

		_ = windows.UnmapViewOfFile(base)
		_ = windows.CloseHandle(mapping)

		if wgcCmd.Process != nil {
			_ = wgcCmd.Process.Kill()
		}

		return fmt.Errorf(
			"selected window has invalid size",
		)
	}

	// The ROI picker intentionally stores client-area coordinates. Determine
	// whether this WGC implementation captured the client or full window, then
	// use the corresponding scale and origin. Using GetWindowRect unconditionally
	// made selections near the screen edge drift on Windows 10.
	clientRect, err := getPickerWindowRect(hwnd)
	if err != nil {
		_ = windows.UnmapViewOfFile(base)
		_ = windows.CloseHandle(mapping)
		if wgcCmd.Process != nil {
			_ = wgcCmd.Process.Kill()
		}
		return fmt.Errorf("GetClientRect failed: %w", err)
	}
	clientWidth := int(clientRect.Right - clientRect.Left)
	clientHeight := int(clientRect.Bottom - clientRect.Top)
	frameMapping, err := frameMappingForWindow(
		reader.width, reader.height,
		windowWidth, windowHeight,
		int(clientRect.Left)-int(rect.Left), int(clientRect.Top)-int(rect.Top),
		clientWidth, clientHeight,
	)
	if err != nil {
		_ = windows.UnmapViewOfFile(base)
		_ = windows.CloseHandle(mapping)
		if wgcCmd.Process != nil {
			_ = wgcCmd.Process.Kill()
		}
		return fmt.Errorf("WGC coordinate mapping failed: %w", err)
	}

	scaleX := frameMapping.ScaleX
	scaleY := frameMapping.ScaleY

	// ========================================================
	// Publish Runtime State
	// ========================================================

	m.mu.Lock()

	m.mapping = mapping
	m.memory = base
	m.reader = reader

	m.bot = bot
	m.autoAccept = autoAccept
	m.autoPot = autoPot
	m.emergency = emergency
	m.deathPause = deathPause
	m.targetUntil = targetUntil

	m.rect = rect

	m.scaleX = scaleX
	m.scaleY = scaleY
	m.frameOriginX = frameMapping.OriginX
	m.frameOriginY = frameMapping.OriginY
	m.captureOffsetX = 0
	m.captureOffsetY = 0

	m.running = true

	m.mu.Unlock()

	// ========================================================
	// Runtime Information
	// ========================================================

	fmt.Println()
	fmt.Println("----------------------------------------")
	fmt.Println("WGC Connected")
	fmt.Println("----------------------------------------")

	fmt.Printf(
		"HWND       : 0x%X\n",
		hwnd,
	)

	fmt.Printf(
		"Resolution : %dx%d\n",
		reader.width,
		reader.height,
	)

	fmt.Printf(
		"Stride     : %d\n",
		reader.stride,
	)

	fmt.Printf(
		"Scale X    : %.4f\n",
		scaleX,
	)

	fmt.Printf(
		"Scale Y    : %.4f\n",
		scaleY,
	)

	fmt.Printf(
		"ROI Basis  : %s | Origin=%d,%d\n",
		map[bool]string{true: "client", false: "window"}[frameMapping.UsesClientFrame],
		frameMapping.OriginX,
		frameMapping.OriginY,
	)

	fmt.Println()

	// ========================================================
	// Start Bot
	// ========================================================

	bot.Start()
	if targetUntil.StartInitialTarget() {
		if bot.CastTarget() {
			appendOCRLog("TARGET UNTIL DEAD | Initial target | Target sent")
		}
	}

	// ========================================================
	// Start Detector / OCR Loop
	// ========================================================

	go func() {

		defer close(doneCh)

		runPicker(
			reader,
			windows.Handle(hwnd),
			rect,
			scaleX,
			scaleY,
			frameMapping.OriginX,
			frameMapping.OriginY,
			bot,
			autoAccept,
			autoPot,
			emergency,
			deathPause,
			targetUntil,
			func(x, y int) { m.setCaptureOffset(x, y) },
			func() { go m.Stop() },
			stopCh,
		)

	}()

	// One evidence set per runtime start for areas selected before Start. This
	// is deliberately not a continuous capture loop.
	go func() {
		select {
		case <-time.After(750 * time.Millisecond):
			m.CaptureConfiguredWGCROISnapshots()
		case <-stopCh:
		}
	}()

	return nil
}

// ============================================================
// STOP
// ============================================================

func (m *RuntimeManager) Stop() {

	m.mu.Lock()

	if !m.running &&
		!m.starting {

		m.mu.Unlock()

		return
	}

	bot := m.bot
	autoAccept := m.autoAccept
	autoPot := m.autoPot
	wgcCmd := m.wgcCmd

	mapping := m.mapping
	memory := m.memory

	stopCh := m.stopCh
	doneCh := m.doneCh

	m.running = false

	m.bot = nil
	m.autoAccept = nil
	m.autoPot = nil
	m.emergency = nil
	m.deathPause = nil
	m.targetUntil = nil
	m.wgcCmd = nil

	m.mapping = 0
	m.memory = 0
	m.reader = nil
	m.frameOriginX = 0
	m.frameOriginY = 0
	m.captureOffsetX = 0
	m.captureOffsetY = 0

	m.hwnd = 0

	m.stopCh = nil
	m.doneCh = nil

	m.mu.Unlock()

	fmt.Println()
	fmt.Println("----------------------------------------")
	fmt.Println("Stopping KaTools Runtime")
	fmt.Println("----------------------------------------")

	// ========================================================
	// Stop detector / OCR loop
	// ========================================================

	if stopCh != nil {
		close(stopCh)
	}

	if doneCh != nil {

		select {

		case <-doneCh:

		case <-time.After(
			1 * time.Second,
		):
		}
	}

	// ========================================================
	// Stop Bot
	// ========================================================

	if bot != nil {
		bot.Stop()
	}

	_ = autoAccept
	_ = autoPot

	// ========================================================
	// Unmap Shared Memory
	// ========================================================

	if memory != 0 {
		_ = windows.UnmapViewOfFile(memory)
	}

	if mapping != 0 {
		_ = windows.CloseHandle(mapping)
	}

	// ========================================================
	// Stop WGC
	// ========================================================

	if wgcCmd != nil &&
		wgcCmd.Process != nil {

		_ = wgcCmd.Process.Kill()
		_ = wgcCmd.Wait()

		fmt.Println("[WGC] Stopped.")
	}

	fmt.Println("[KaTools] Runtime stopped.")
}

// ============================================================
// STATUS
// ============================================================

func (m *RuntimeManager) Status() map[string]interface{} {

	m.mu.RLock()
	defer m.mu.RUnlock()

	title := ""

	if m.hwnd != 0 {
		title =
			getWindowTitle(
				windows.Handle(m.hwnd),
			)
	}

	botRunning := false

	if m.bot != nil {
		botRunning =
			m.bot.IsRunning()
	}

	autoPotStatus := AutoPotStatus{}
	if m.autoPot != nil {
		autoPotStatus = m.autoPot.Status()
	}

	return map[string]interface{}{

		"running": m.running,

		"starting": m.starting,

		"hwnd": func() string {

			if m.hwnd == 0 {
				return ""
			}

			return fmt.Sprintf(
				"0x%X",
				m.hwnd,
			)
		}(),

		"title": title,

		"botRunning": botRunning,
		"paused":     m.running && !botRunning,

		"autoPot": autoPotStatus,
	}
}

// ============================================================
// BOT ACCESS
// ============================================================

func (m *RuntimeManager) Bot() *BotController {

	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.bot
}

func (m *RuntimeManager) AutoAccept() *AutoAcceptController {

	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.autoAccept
}

// UpdateConfig applies UI changes without restarting WGC or the runtime. The
// bot workers are restarted internally so enabled flags, delays, and the skill
// list all take effect immediately and no old scheduler remains active.
func (m *RuntimeManager) UpdateConfig(cfg WebBotConfig) error {
	m.mu.RLock()

	if !m.running || m.bot == nil || m.autoAccept == nil || m.autoPot == nil || m.emergency == nil || m.deathPause == nil || m.targetUntil == nil {
		m.mu.RUnlock()
		return fmt.Errorf("KaTools is not running")
	}

	if cfg.HWND != "" {
		hwnd, err := parseHWND(cfg.HWND)
		if err != nil || hwnd != m.hwnd {
			m.mu.RUnlock()
			return fmt.Errorf("target window cannot be changed while running")
		}
	}

	bot := m.bot
	autoAccept := m.autoAccept
	autoPot := m.autoPot
	emergency := m.emergency
	deathPause := m.deathPause
	targetUntil := m.targetUntil
	hwnd := m.hwnd
	targetFilterEnabled := cfg.TargetUntilDeadEnabled ||
		(cfg.TargetEnabled && strings.TrimSpace(cfg.TargetUntilDeadCharacterName) != "")
	captureModeChanged := (cfg.AutoAcceptEnabled || cfg.AutoPotHPEnabled || cfg.AutoPotTPEnabled || cfg.AutoPauseDeathEnabled || targetFilterEnabled) !=
		(autoAccept.IsEnabled() || autoPot.IsAnyEnabled() || deathPause.IsEnabled() || targetUntil.IsFilterEnabled())
	m.mu.RUnlock()

	// Toggling Auto Accept changes whether WGC is required at all. Recreate the
	// runtime so checking it starts capture and unchecking it releases capture.
	if captureModeChanged {
		m.Stop()
		return m.Start(hwnd, cfg)
	}

	bot.Stop()
	applyWebBotConfig(bot, autoAccept, autoPot, emergency, deathPause, targetUntil, cfg)
	bot.Start()

	return nil
}

func (m *RuntimeManager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// ============================================================
// WINDOW
// ============================================================

func isWindowValid(
	hwnd uintptr,
) bool {

	if hwnd == 0 {
		return false
	}

	user32 :=
		windows.NewLazySystemDLL(
			"user32.dll",
		)

	proc :=
		user32.NewProc(
			"IsWindow",
		)

	ret, _, _ :=
		proc.Call(hwnd)

	return ret != 0
}

func isWindowForeground(hwnd windows.Handle) bool {
	if hwnd == 0 {
		return false
	}

	user32 := windows.NewLazySystemDLL("user32.dll")
	getForegroundWindow := user32.NewProc("GetForegroundWindow")

	foreground, _, _ := getForegroundWindow.Call()
	return foreground == uintptr(hwnd)
}

func getTargetFocusInfo(hwnd windows.Handle) (uintptr, string, string, error) {
	if hwnd == 0 {
		return 0, "", "", fmt.Errorf("target window is zero")
	}

	user32 := windows.NewLazySystemDLL("user32.dll")
	getWindowThreadProcessID := user32.NewProc("GetWindowThreadProcessId")
	getGUIThreadInfo := user32.NewProc("GetGUIThreadInfo")
	getClassName := user32.NewProc("GetClassNameW")

	threadID, _, _ := getWindowThreadProcessID.Call(uintptr(hwnd), 0)
	if threadID == 0 {
		return 0, "", "", fmt.Errorf("GetWindowThreadProcessId failed")
	}

	info := GUIThreadInfo{CbSize: uint32(unsafe.Sizeof(GUIThreadInfo{}))}
	if ret, _, err := getGUIThreadInfo.Call(
		threadID,
		uintptr(unsafe.Pointer(&info)),
	); ret == 0 {
		return 0, "", "", fmt.Errorf("GetGUIThreadInfo failed: %v", err)
	}

	if info.HwndFocus == 0 {
		return 0, "", "", nil
	}

	classBuffer := make([]uint16, 256)
	classLength, _, _ := getClassName.Call(
		info.HwndFocus,
		uintptr(unsafe.Pointer(&classBuffer[0])),
		uintptr(len(classBuffer)),
	)

	className := ""
	if classLength > 0 {
		className = windows.UTF16ToString(classBuffer[:classLength])
	}

	return info.HwndFocus,
		className,
		getWindowTitle(windows.Handle(info.HwndFocus)), nil
}

func getWindowTitle(
	hwnd windows.Handle,
) string {

	user32 :=
		windows.NewLazySystemDLL(
			"user32.dll",
		)

	proc :=
		user32.NewProc(
			"GetWindowTextW",
		)

	lengthProc :=
		user32.NewProc(
			"GetWindowTextLengthW",
		)

	length, _, _ :=
		lengthProc.Call(
			uintptr(hwnd),
		)

	if length == 0 {
		return ""
	}

	buffer :=
		make(
			[]uint16,
			int(length)+1,
		)

	proc.Call(
		uintptr(hwnd),
		uintptr(
			unsafe.Pointer(
				&buffer[0],
			),
		),
		uintptr(len(buffer)),
	)

	return windows.UTF16ToString(buffer)
}

// ============================================================
// SHARED MEMORY WAIT
// ============================================================

func waitForSharedMemoryWithStop(
	stopCh <-chan struct{},
	timeout time.Duration,
) (windows.Handle, error) {

	deadline :=
		time.Now().Add(timeout)

	for {

		select {

		case <-stopCh:
			return 0, fmt.Errorf(
				"runtime stopped",
			)

		default:
		}

		handle, err :=
			openSharedMemory()

		if err == nil {
			return handle, nil
		}

		if time.Now().After(deadline) {

			return 0, fmt.Errorf(
				"timeout after %s",
				timeout,
			)
		}

		time.Sleep(
			200 * time.Millisecond,
		)
	}
}

// ============================================================
// DEFAULT BOT CONFIG
// ============================================================

func newDefaultBotConfig() BotConfig {

	return BotConfig{

		Enabled: true,

		AssistEnabled:    false,
		AssistSkillVK:    0,
		AssistSkillDelay: 1 * time.Second,

		TargetEnabled: false,
		TargetDelay:   2 * time.Second,

		AttackEnabled: false,
		AttackDelay:   500 * time.Millisecond,

		PickEnabled: false,
		PickDelay:   1 * time.Second,

		Skills: []SkillConfig{

			{Name: "1", VK: 0x31},
			{Name: "2", VK: 0x32},
			{Name: "3", VK: 0x33},
			{Name: "4", VK: 0x34},
			{Name: "5", VK: 0x35},
			{Name: "6", VK: 0x36},
			{Name: "7", VK: 0x37},
			{Name: "8", VK: 0x38},
			{Name: "9", VK: 0x39},
			{Name: "0", VK: 0x30},

			{Name: "F1", VK: 0x70},
			{Name: "F2", VK: 0x71},
			{Name: "F3", VK: 0x72},
			{Name: "F4", VK: 0x73},
			{Name: "F5", VK: 0x74},
			{Name: "F6", VK: 0x75},
			{Name: "F7", VK: 0x76},
			{Name: "F8", VK: 0x77},
			{Name: "F9", VK: 0x78},
			{Name: "F10", VK: 0x79},
		},
	}
}

// ============================================================
// SHUTDOWN
// ============================================================

func setupShutdown(
	runtimeManager *RuntimeManager,
) {

	sigChan :=
		make(
			chan os.Signal,
			1,
		)

	signal.Notify(
		sigChan,
		os.Interrupt,
		syscall.SIGTERM,
	)

	go func() {

		<-sigChan

		fmt.Println()
		fmt.Println("----------------------------------------")
		fmt.Println("Shutting down KaTools...")
		fmt.Println("----------------------------------------")

		runtimeManager.Stop()

		os.Exit(0)

	}()
}

// ============================================================
// ENTER
// ============================================================

func pressEnterToWindow(
	hwnd uintptr,
) bool {

	if hwnd == 0 {
		return false
	}

	user32 :=
		windows.NewLazySystemDLL(
			"user32.dll",
		)

	postMessage :=
		user32.NewProc(
			"PostMessageW",
		)

	// ENTER scan code = 0x1C.
	keyDownLParam :=
		uintptr(
			1 |
				(0x1C << 16),
		)

	keyUpLParam :=
		uintptr(
			1 |
				(0x1C << 16) |
				(1 << 30) |
				(1 << 31),
		)

	retDown, _, _ :=
		postMessage.Call(
			hwnd,
			uintptr(wmKeyDown),
			uintptr(vkReturn),
			keyDownLParam,
		)

	time.Sleep(
		30 * time.Millisecond,
	)

	retUp, _, _ :=
		postMessage.Call(
			hwnd,
			uintptr(wmKeyUp),
			uintptr(vkReturn),
			keyUpLParam,
		)

	return retDown != 0 &&
		retUp != 0
}

// ============================================================
// PICKER + PARTY DETECTOR + OCR
// ============================================================

func runPicker(

	reader *FrameReader,
	hwnd windows.Handle,
	rect RECT,
	scaleX float64,
	scaleY float64,
	frameOriginX int,
	frameOriginY int,
	bot *BotController,
	autoAccept *AutoAcceptController,
	autoPot *AutoPotController,
	emergency *EmergencySkillController,
	deathPause *DeathPauseController,
	targetUntil *TargetUntilDeadController,
	onCaptureOffset func(x, y int),
	onDeath func(),
	stopCh <-chan struct{},
) {

	user32 :=
		windows.NewLazySystemDLL(
			"user32.dll",
		)

	getCursorPos :=
		user32.NewProc(
			"GetCursorPos",
		)

	getAsyncKeyState :=
		user32.NewProc(
			"GetAsyncKeyState",
		)

	var previousF8 bool
	var previousF9 bool
	var lastFocusPoll time.Time
	var lastFocusHWND uintptr
	var lastFocusClass string
	var lastFocusText string

	// ========================================================
	// PARTY DETECTOR
	// ========================================================

	// ========================================================
	// OCR
	// ========================================================
	//
	// OCR configuration sekarang dikelola oleh ocr.go.
	//
	// LoadPartyROI() akan:
	// - membaca party_roi.json
	// - atau membuat default
	//
	tesseractPath := resolveTesseractPath()
	ocr := ocrworker.New(tesseractPath)
	statusOCR := ocrworker.New(tesseractPath)
	deathOCR := ocrworker.New(tesseractPath)
	resurrectionOCR := ocrworker.New(tesseractPath)
	targetNameOCR := ocrworker.New(tesseractPath)
	_ = hideROIConfigFiles()

	ocrROI := partyROIToFrame(
		ocrworker.LoadPartyROI(),
		scaleX,
		scaleY,
		frameOriginX,
		frameOriginY,
	)

	popupROI := automaticPopupROI(reader.width, reader.height)
	popupDetector := newPartyDetector(popupROI)
	partyMembership := &PartyMembershipMonitor{}

	// ========================================================
	// FRAME STATE
	// ========================================================

	var lastFrameNumber uint64
	var lastDetectorRun time.Time

	var lastPopupState bool

	// ========================================================
	// OCR STATE
	// ========================================================
	//
	// OCR dijalankan async supaya Tesseract yang lambat
	// tidak menghentikan WGC / detector loop.
	//
	type ocrEvent struct {
		frame        uint64
		text         string
		party        bool
		variantPhase int
		dur          time.Duration
		err          error
	}

	ocrResultCh :=
		make(chan ocrEvent, 4)

	ocrBusy := false

	var lastOCRStart time.Time
	var lastEnterAt time.Time
	// OCR should be fast after the visual detector sees a potential party
	// popup, but there is no need to start a costly Tesseract process twice a
	// second while the screen is normal. The occasional full fallback keeps
	// detection working if a non-standard dialog escapes the visual detector.
	const partyOCRActiveInterval = 500 * time.Millisecond
	const partyOCRPassiveInterval = 3 * time.Second
	const partyOCRFullFallbackInterval = 12 * time.Second
	partyOCRVariantPhase := 0
	lastPartyFullOCRAt := time.Now()

	type statusOCREvent struct {
		text      string
		hpText    string
		tpText    string
		err       error
		offsetX   int
		offsetY   int
		hpPercent float64
		hpOK      bool
		tpPercent float64
		tpOK      bool
	}
	statusOCRResultCh := make(chan statusOCREvent, 1)
	statusOCRBusy := false
	type targetNameOCREvent struct {
		generation uint64
		revision   uint64
		text       string
		err        error
	}
	targetNameOCRResultCh := make(chan targetNameOCREvent, 1)
	targetNameOCRBusy := false
	type deathOCREvent struct {
		text       string
		err        error
		dialogMask deathDialogMask
	}
	deathOCRResultCh := make(chan deathOCREvent, 1)
	deathOCRBusy := false
	type resurrectionOCREvent struct {
		text       string
		err        error
		foreground bool
	}
	resurrectionOCRResultCh := make(chan resurrectionOCREvent, 1)
	resurrectionOCRBusy := false
	var lastStatusBarAt time.Time
	var lastStatusOCRAt time.Time
	var lastStatusDiagnosticAt time.Time
	// WGC can be shifted relative to the client-area picker on certain Windows
	// / GPU combinations. This is one two-axis capture-coordinate correction,
	// not a Status-only setting: every user-selected ROI uses it.
	captureOffsetX := 0
	captureOffsetY := 0
	captureOffsetCalibrated := false
	captureOffsetSearchIndex := 0
	var statusCalibrationROI ocrworker.PartyROIConfig
	statusCalibrationROISet := false
	var lastStatusFrameNumber uint64
	var lastPartyPanelCheck time.Time
	var lastTargetBarAt time.Time
	var lastDeathOCRAt time.Time
	var lastResurrectionOCRAt time.Time
	var lastTargetNameSignature uint64
	var targetNameSignatureSet bool
	var pendingTargetNameSignature uint64
	var pendingTargetNameSignatureScans int
	var lastTargetNameOCRAt time.Time
	var targetNameVerified bool
	var targetIsOwnCharacter bool
	var targetNameValidationLogged bool
	lastTargetGeneration := bot.TargetGeneration()
	var targetValidationRevision uint64
	var targetValidationReadyAt time.Time
	var targetValidationStartedAt time.Time
	// Status scanning is independent from the heavier party-popup detector.
	// It is cheap enough to run on every fresh WGC frame, capped at 10 Hz.
	const statusBarInterval = 100 * time.Millisecond
	// A verified numeric read calibrates the fast colour-bar estimate. Once
	// maxima are known, live HP/TP comes from the colour bars at 10 Hz, so a
	// numeric refresh every 30 seconds is enough. Status OCR remains
	// independent from party, target, death, and resurrection OCR so one
	// detector can never make another wait.
	const statusOCRRefreshInterval = 30 * time.Second
	// The desktop picker and WGC can differ by a few pixels on some games or
	// Windows/DPI combinations. Probe nearby crops until OCR finds two bar
	// values that agree with their colored fills, then retain the 2D offset.
	statusOffsetCandidates := statusCaptureOffsetCandidates()
	applyCaptureOffset := func(roi ocrworker.PartyROIConfig) ocrworker.PartyROIConfig {
		roi.X += captureOffsetX
		roi.Y += captureOffsetY
		return roi
	}
	const partyPanelCheckInterval = 1500 * time.Millisecond
	const targetBarInterval = 250 * time.Millisecond
	const targetNameOCRMinInterval = 500 * time.Millisecond
	const targetValidationSettleDelay = 180 * time.Millisecond
	// Target name OCR launches Tesseract as a process. On a slower Windows 10
	// disk/CPU it can take longer than the old 1.4-second allowance, even when
	// the WGC crop contains a perfectly readable name. Give it time to return
	// and never retarget while that OCR request is still in flight.
	const targetValidationTimeout = 3 * time.Second
	// Death dialogs are much smaller than party popups, so their visual shape
	// does not reliably satisfy PartyDetector's popup thresholds. Probe only the
	// chosen Death ROI directly instead; the strict respawn-text check below is
	// what authorizes a pause.
	const deathOCRMinInterval = 1500 * time.Millisecond
	const resurrectionOCRMinInterval = 600 * time.Millisecond

	logAutoPotEvents := func(events []AutoPotEvent) {
		for _, potEvent := range events {
			appendOCRLog(
				"AUTO POT | %s=%d/%d (%.1f%%) <= %.1f%% | SlotVK=%d | Sent=%t",
				potEvent.Resource,
				potEvent.Current,
				potEvent.Maximum,
				float64(potEvent.Current)*100.0/float64(potEvent.Maximum),
				potEvent.Threshold,
				potEvent.SlotVK,
				potEvent.Pressed,
			)
		}
	}

	// Status OCR refreshes frequently. Keep its diagnostics actionable without
	// letting transient OCR misses drown out potion, death, and target events.
	logStatusDiagnostic := func(format string, args ...interface{}) {
		if time.Since(lastStatusDiagnosticAt) < 30*time.Second {
			return
		}
		lastStatusDiagnosticAt = time.Now()
		appendOCRLog("STATUS OCR | "+format, args...)
	}

	// High-priority HP/TP path. Keep it outside the Party detector throttle so
	// potion and emergency actions never wait for popup analysis.
	scanStatusBar := func() {
		if autoPot == nil {
			return
		}
		watchDeathRecovery := deathPause != nil && deathPause.ShouldWatchHP() &&
			autoPot.ConfiguredHPThreshold() > 0
		pickedStatusROI := LoadStatusROI()
		calibrationPending := !captureOffsetCalibrated && pickedStatusROI.Selected
		if (!autoPot.IsAnyEnabled() && !watchDeathRecovery && !calibrationPending) ||
			time.Since(lastStatusBarAt) < statusBarInterval {
			return
		}

		statusROI := partyROIToFrame(pickedStatusROI, scaleX, scaleY, frameOriginX, frameOriginY)
		if !statusROI.Selected {
			logStatusDiagnostic("no status ROI selected")
			return
		}
		if !statusCalibrationROISet || statusCalibrationROI != statusROI {
			// A newly picked ROI (or changed WGC scale) needs a fresh alignment.
			// Do not retain maxima from the former crop during that short probe.
			statusCalibrationROI = statusROI
			statusCalibrationROISet = true
			captureOffsetX = 0
			captureOffsetY = 0
			if onCaptureOffset != nil {
				onCaptureOffset(captureOffsetX, captureOffsetY)
			}
			captureOffsetCalibrated = false
			captureOffsetSearchIndex = 0
			lastStatusOCRAt = time.Time{}
			autoPot.ResetStatus()
			appendOCRLog("STATUS OCR | status ROI changed; recalibrating WGC X/Y offset")
		}
		statusScanOffset := captureOffset{X: captureOffsetX, Y: captureOffsetY}
		if !captureOffsetCalibrated {
			statusScanOffset = statusOffsetCandidates[captureOffsetSearchIndex%len(statusOffsetCandidates)]
		}
		statusROI.X += statusScanOffset.X
		statusROI.Y += statusScanOffset.Y
		img, ok := reader.ToImageRect(statusROI.X, statusROI.Y, statusROI.Width, statusROI.Height)
		if !ok {
			logStatusDiagnostic(
				"ROI outside WGC frame | ROI=%d,%d %dx%d | OffsetX=%d OffsetY=%d | Frame=%dx%d | Scale=%.4f,%.4f",
				statusROI.X, statusROI.Y, statusROI.Width, statusROI.Height,
				statusScanOffset.X, statusScanOffset.Y,
				reader.width, reader.height, scaleX, scaleY,
			)
			return
		}

		lastStatusBarAt = time.Now()
		hpPercent, hpOK, tpPercent, tpOK := detectStatusBarPercents(img)
		// While calibrating, a crop without both coloured bars is definitely not
		// the status panel. Move to the next 2D candidate without spending a
		// Tesseract process on terrain or empty UI.
		statusCandidateReady := captureOffsetCalibrated || (hpOK && tpOK)
		if !captureOffsetCalibrated && !statusCandidateReady {
			captureOffsetSearchIndex++
		}
		if statusCandidateReady &&
			(!autoPot.HasStatusMaxima() || time.Since(lastStatusOCRAt) >= statusOCRRefreshInterval) &&
			!statusOCRBusy {
			statusOCRBusy = true
			lastStatusOCRAt = time.Now()
			if !captureOffsetCalibrated {
				captureOffsetSearchIndex++
			}
			go func(img image.Image, offset captureOffset, hpPercent float64, hpOK bool, tpPercent float64, tpOK bool) {
				result, err := statusOCR.RunStatusImage(img)
				event := statusOCREvent{err: err, offsetX: offset.X, offsetY: offset.Y, hpPercent: hpPercent, hpOK: hpOK, tpPercent: tpPercent, tpOK: tpOK}
				if result != nil {
					event.text = result.Text
					event.hpText = result.StatusHPText
					event.tpText = result.StatusTPText
				}
				select {
				case statusOCRResultCh <- event:
				case <-stopCh:
				}
			}(img, statusScanOffset, hpPercent, hpOK, tpPercent, tpOK)
		}

		deathPaused := deathPause != nil && deathPause.IsTriggered()
		if !deathPaused {
			logAutoPotEvents(autoPot.ObserveBarPercents(hpPercent, hpOK, tpPercent, tpOK))
		}
		if !deathPaused && emergency != nil && bot.IsRunning() {
			if emergency.ObserveHPPercent(hpPercent, hpOK, autoPot.HPThreshold()) {
				// A target-required Emergency skill is allowed to acquire a
				// target normally. The sole exception is Support's confirmed
				// target-clear window: wait until its held non-With Target skills
				// are flushed, otherwise this E can recreate the same post-death
				// race that Support mode prevents for Target Until Dead itself.
				if bot.shouldDeferEmergencyTarget() {
					emergency.MarkTargetRequestFailed()
				} else if bot.CastTarget() {
					appendOCRLog("EMERGENCY | Panic target requested | E sent")
				} else {
					emergency.MarkTargetRequestFailed()
				}
			}
		}
		if deathPause != nil && deathPause.ObserveHPPercent(
			hpPercent, hpOK, autoPot.ConfiguredHPThreshold(),
		) {
			appendOCRLog("AUTO RESUME | HP %.1f%% is above Auto Potion threshold | Bot resumed", hpPercent)
			bot.Start()
		}
	}

	// ========================================================
	// OCR START
	// ========================================================

	startOCR :=
		func(frameNumber uint64, popupActive bool) {

			if ocrBusy {
				return
			}

			minInterval := partyOCRPassiveInterval
			if popupActive {
				minInterval = partyOCRActiveInterval
			}
			if time.Since(lastOCRStart) < minInterval {

				return
			}

			// Variant 0 is the normal grayscale image and handles the usual
			// party dialog. Remaining threshold variants are deliberately
			// deferred until that fast verification was inconclusive, or to an
			// occasional background fallback when the visual detector misses.
			variantPhase := 0
			variantIndexes := []int{0}
			if popupActive && partyOCRVariantPhase == 1 {
				variantPhase = 1
				variantIndexes = []int{1, 2, 3, 4}
				lastPartyFullOCRAt = time.Now()
			} else if !popupActive && time.Since(lastPartyFullOCRAt) >= partyOCRFullFallbackInterval {
				variantPhase = 1
				variantIndexes = []int{1, 2, 3, 4}
				lastPartyFullOCRAt = time.Now()
			}

			// ------------------------------------------------
			// Reload ROI.
			//
			// Jadi kalau party_roi.json berubah,
			// runtime bisa menggunakan konfigurasi terbaru
			// tanpa harus mengubah source main.go.
			// ------------------------------------------------

			roi := partyROIToFrame(
				ocrworker.LoadPartyROI(),
				scaleX,
				scaleY,
				frameOriginX,
				frameOriginY,
			)
			roi = applyCaptureOffset(roi)

			img, ok :=
				reader.ToImageRect(
					roi.X,
					roi.Y,
					roi.Width,
					roi.Height,
				)

			if !ok {

				appendOCRLog(
					"OCR IMAGE ERROR | Frame=%d | ROI=(%d,%d,%d,%d)",
					frameNumber,
					roi.X,
					roi.Y,
					roi.Width,
					roi.Height,
				)

				return
			}

			ocrBusy = true
			lastOCRStart = time.Now()

			go func(
				frame uint64,
				img image.Image,
				variantPhase int,
				variantIndexes []int,
			) {

				started :=
					time.Now()

				result, err :=
					ocr.RunPartyImageVariants(img, variantIndexes)

				duration :=
					time.Since(started)

				if err != nil {

					appendOCRLog(
						"OCR ERROR | Frame=%d | Time=%s | Error=%v",
						frame,
						duration,
						err,
					)

					select {

					case ocrResultCh <- ocrEvent{
						frame:        frame,
						variantPhase: variantPhase,
						dur:          duration,
						err:          err,
					}:

					case <-stopCh:
					}

					return
				}

				if result == nil {

					err =
						fmt.Errorf(
							"ocr result is nil",
						)

					appendOCRLog(
						"OCR ERROR | Frame=%d | Time=%s | Result=nil",
						frame,
						duration,
					)

					select {

					case ocrResultCh <- ocrEvent{
						frame:        frame,
						variantPhase: variantPhase,
						dur:          duration,
						err:          err,
					}:

					case <-stopCh:
					}

					return
				}

				select {

				case ocrResultCh <- ocrEvent{
					frame:        frame,
					text:         result.Text,
					party:        result.IsParty,
					variantPhase: variantPhase,
					dur:          duration,
				}:

				case <-stopCh:
				}

			}(
				frameNumber,
				img,
				variantPhase,
				variantIndexes,
			)
		}

	// ========================================================
	// START MESSAGE
	// ========================================================

	resetOCRLog()

	fmt.Println("----------------------------------------")
	fmt.Println("PartyDetector + OCR v6")
	fmt.Println("----------------------------------------")

	fmt.Printf(
		"Detector ROI : automatic center popup area\n",
	)

	fmt.Printf(
		"OCR ROI      : X=%d Y=%d W=%d H=%d\n",
		ocrROI.X,
		ocrROI.Y,
		ocrROI.Width,
		ocrROI.Height,
	)

	fmt.Println(
		"Detector     : visual difference + hysteresis",
	)

	fmt.Println(
		"OCR          : async + ocrworker.RunImage",
	)

	fmt.Println(
		"AutoAccept   : OCR confirmed -> ENTER",
	)

	fmt.Println(
		"Status       : BUILDING BASELINE...",
	)

	fmt.Println()

	// ========================================================
	// MAIN LOOP
	// ========================================================

	for {

		select {

		case <-stopCh:
			return

		default:
		}

		// Status OCR runs independently from the party popup OCR. The status ROI
		// contains the red HP and blue TP lines in that order.
		select {
		case event := <-statusOCRResultCh:
			statusOCRBusy = false
			if event.err != nil {
				logStatusDiagnostic("Tesseract failed | %v", event.err)
				break
			}
			hpCurrent, hpMax, hpParsed := parseStatusPair(event.hpText)
			tpCurrent, tpMax, tpParsed := parseStatusPair(event.tpText)
			// The two-line fallback has no row labels, but remains useful when it
			// successfully reads both pairs. Row-specific results always win so a
			// strong HP read can be combined with TP from another image variant.
			if !hpParsed || !tpParsed {
				fallbackHPCurrent, fallbackHPMax, fallbackTPCurrent, fallbackTPMax, fallbackOK := parseStatusPairs(event.text)
				if fallbackOK {
					if !hpParsed {
						hpCurrent, hpMax, hpParsed = fallbackHPCurrent, fallbackHPMax, true
					}
					if !tpParsed {
						tpCurrent, tpMax, tpParsed = fallbackTPCurrent, fallbackTPMax, true
					}
				}
			}
			if !hpParsed && !tpParsed {
				logStatusDiagnostic(
					"could not parse HP or TP pair | OffsetX=%d OffsetY=%d | Text=%q | Bar HP=%.1f%% ok=%t TP=%.1f%% ok=%t",
					event.offsetX, event.offsetY, event.text, event.hpPercent, event.hpOK, event.tpPercent, event.tpOK,
				)
				break
			}
			deathPaused := deathPause != nil && deathPause.IsTriggered()
			if autoPot != nil && !deathPaused {
				// OCR is used to establish the fixed HP/TP maxima and calibrate
				// the coloured bars. Once both maxima are known, its small HUD
				// digits must not overwrite the live current values: a moving game
				// camera changes the transparent background behind those digits and
				// can make Tesseract read (for example) 2314 instead of 2414.
				statusMaximaKnown := autoPot.HasStatusMaxima()
				rawHPCurrent, rawHPMax := hpCurrent, hpMax
				rawTPCurrent, rawTPMax := tpCurrent, tpMax
				hpMatchesBar := hpParsed && statusPairMatchesBar(hpCurrent, hpMax, event.hpPercent, event.hpOK)
				tpMatchesBar := tpParsed && statusPairMatchesBar(tpCurrent, tpMax, event.tpPercent, event.tpOK)
				if !hpMatchesBar {
					if hpParsed {
						logStatusDiagnostic("HP current rejected by bar validation | OCR=%d/%d Bar=%.1f%% ok=%t", hpCurrent, hpMax, event.hpPercent, event.hpOK)
					}
					hpCurrent, hpMax = 0, 0
				}
				if !tpMatchesBar {
					if tpParsed {
						logStatusDiagnostic("TP current rejected by bar validation | OCR=%d/%d Bar=%.1f%% ok=%t", tpCurrent, tpMax, event.tpPercent, event.tpOK)
					}
					tpCurrent, tpMax = 0, 0
				}
				// A repeated N/N read establishes the fixed maximum even if OCR's
				// current number is inconsistent with the coloured bar. This never
				// authorizes a potion from OCR current; ObserveBarPercents remains
				// the source of live HP/TP after a maximum is learned.
				hpLearned, tpLearned := autoPot.LearnFullStatusMaximums(
					rawHPCurrent, rawHPMax,
					rawTPCurrent, rawTPMax,
				)
				if hpLearned || tpLearned {
					appendOCRLog("STATUS OCR | maximum learned from repeated full value | HP=%t TP=%t", hpLearned, tpLearned)
				}
				if !statusMaximaKnown {
					autoPot.CalibrateBarPercents(
						hpCurrent, hpMax, event.hpPercent, hpMatchesBar,
						tpCurrent, tpMax, event.tpPercent, tpMatchesBar,
					)
				}
				if hpMatchesBar && tpMatchesBar {
					offsetChanged := !captureOffsetCalibrated || captureOffsetX != event.offsetX || captureOffsetY != event.offsetY
					captureOffsetX = event.offsetX
					captureOffsetY = event.offsetY
					if onCaptureOffset != nil {
						onCaptureOffset(captureOffsetX, captureOffsetY)
					}
					captureOffsetCalibrated = true
					if offsetChanged {
						appendOCRLog(
							"WGC CALIBRATION | X/Y offset locked | OffsetX=%d OffsetY=%d | Applied to Status, Party, Death, Target",
							captureOffsetX, captureOffsetY,
						)
					}
				}
				if !statusMaximaKnown && (hpMatchesBar || tpMatchesBar) {
					// The first good OCR result seeds maxima. From the next WGC frame
					// onward ObserveBarPercents supplies the stable live values.
					logAutoPotEvents(autoPot.Observe(hpCurrent, hpMax, tpCurrent, tpMax))
				} else if autoPot.HasStatusMaxima() {
					logAutoPotEvents(autoPot.ObserveBarPercents(event.hpPercent, event.hpOK, event.tpPercent, event.tpOK))
				}
			}
		default:
		}

		select {
		case event := <-targetNameOCRResultCh:
			targetNameOCRBusy = false
			if event.generation != bot.TargetGeneration() || event.revision != targetValidationRevision || targetUntil == nil {
				break
			}
			if event.err != nil {
				if !targetNameValidationLogged {
					targetNameValidationLogged = true
					appendOCRLog("TARGET VALIDATION | Name OCR failed | %v", event.err)
				}
				break
			}
			if !targetTextHasReadableName(event.text) {
				if !targetNameValidationLogged {
					targetNameValidationLogged = true
					appendOCRLog("TARGET VALIDATION | Name OCR unreadable | Text=%q", oCRTextForLog(event.text))
				}
				break
			}

			targetNameVerified = true
			if targetTextMatchesExcludedName(event.text, targetUntil.ExcludedNames()) {
				targetIsOwnCharacter = true
				bot.SetTargetActionReady(false)
				if targetUntil.ForceRetarget() && bot.CastTarget() {
					appendOCRLog("TARGET UNTIL DEAD | Excluded target skipped | Text=%q | Target sent", oCRTextForLog(event.text))
				}
			} else if targetTextPotentiallyMatchesExcludedName(event.text, targetUntil.ExcludedNames()) {
				// Do not turn a clipped reading of a skipped multi-word target
				// into an allowed target. Wait for another OCR result; the regular
				// validation timeout will safely move to the next target if the
				// full name never becomes readable.
				targetNameVerified = false
				targetIsOwnCharacter = true
				bot.SetTargetActionReady(false)
				if !targetNameValidationLogged {
					targetNameValidationLogged = true
					appendOCRLog("TARGET VALIDATION | Possible excluded target; waiting for complete OCR | Text=%q", oCRTextForLog(event.text))
				}
			} else {
				targetIsOwnCharacter = false
				bot.SetTargetActionReady(true)
				appendOCRLog("TARGET VALIDATION | Allowed target | Text=%q", oCRTextForLog(event.text))
			}
		default:
		}

		// Death OCR only starts after the selected dialog area visibly changes.
		// This keeps idle monitoring to a cheap pixel sample.
		select {
		case event := <-deathOCRResultCh:
			deathOCRBusy = false
			if event.err == nil && deathPause != nil {
				deathTextVisible := isDeathRespawnMessage(event.text)
				if deathTextVisible && deathPause.TriggerOnce() {
					deathPause.RememberDeathDialog(event.dialogMask)
					appendOCRLog(
						"DEATH DETECTED | Auto Pause triggered | Text=%q",
						oCRTextForLog(event.text),
					)
					// Keep the capture monitor alive even when Auto Resu is off. This
					// lets a player click the normal respawn OK button and have KaTools
					// resume only after HP is confirmed healthy.
					bot.Stop()
				}
				deathDialogVisible := deathTextVisible || deathPause.DeathDialogMatches(event.dialogMask)
				deathPause.ObserveDeathDialog(deathDialogVisible)
			}
		default:
		}

		select {
		case event := <-resurrectionOCRResultCh:
			resurrectionOCRBusy = false
			if event.err == nil && deathPause != nil && deathPause.ShouldScanResurrection() &&
				isResurrectionPrompt(event.text) && event.foreground &&
				deathPause.ObserveResurrectionPrompt(true) {
				if pressEnterToWindow(uintptr(hwnd)) && deathPause.MarkResurrectionAccepted() {
					appendOCRLog("AUTO RESU | Resurrection prompt verified | Enter sent | Waiting for HP")
				} else {
					appendOCRLog("AUTO RESU | Resurrection prompt verified | Enter failed")
				}
			} else if deathPause != nil && deathPause.ShouldScanResurrection() {
				visible := event.err == nil && isResurrectionPrompt(event.text) && event.foreground
				deathPause.ObserveResurrectionPrompt(visible)
				if event.err == nil && isResurrectionPrompt(event.text) && !event.foreground {
					appendOCRLog("AUTO RESU | Resurrection text found but dialog is not foreground | Waiting")
				}
			}
		default:
		}

		// A native picker needs responsive mouse input. Pause new OCR/detector
		// work while it is open; the selected ROI is reloaded immediately after
		// the picker closes.
		if isPartyROIPickerOpen() {
			time.Sleep(30 * time.Millisecond)
			continue
		}

		// ====================================================
		// OCR RESULT
		// ====================================================

		select {

		case event := <-ocrResultCh:

			ocrBusy = false
			// If the fast grayscale pass did not verify a popup which is
			// still visually active, let the next active scan spend the extra
			// work on the threshold fallbacks. A completed fallback (or a
			// cleared popup) starts the next dialog from the fast pass again.
			if event.party {
				partyOCRVariantPhase = 0
			} else if lastPopupState && event.variantPhase == 0 {
				partyOCRVariantPhase = 1
			} else {
				partyOCRVariantPhase = 0
			}

			if event.err != nil {

				appendOCRLog(
					"OCR RESULT ERROR | Frame=%d | Time=%s | Error=%v",
					event.frame,
					event.dur,
					event.err,
				)

				break
			}

			// ------------------------------------------------
			// OCR PARTY CONFIRMED
			// ------------------------------------------------

			if event.party {
				fmt.Println()
				fmt.Println("========================================")
				fmt.Println("[PARTY] OCR CONFIRMED")
				fmt.Println("========================================")

				fmt.Printf(
					"[OCR] Text : %s\n",
					event.text,
				)

				fmt.Printf(
					"[OCR] Time : %s\n",
					event.dur,
				)

				appendOCRLog(
					"PARTY DETECTED | Frame=%d | OCR=%s | Text=%q",
					event.frame,
					event.dur,
					oCRTextForLog(event.text),
				)

				// ============================================
				// AUTO ACCEPT
				// ============================================
				if autoAccept != nil &&
					autoAccept.IsEnabled() {

					if time.Since(lastEnterAt) >=
						partyEnterCooldown {

						time.Sleep(
							partyAcceptDelay,
						)

						sent :=
							pressEnterToWindow(
								uintptr(hwnd),
							)

						if sent {

							lastEnterAt =
								time.Now()
							partyMembership.ExpectPanel()

							fmt.Printf(
								"[AutoAccept] ENTER sent to HWND 0x%X\n",
								uintptr(hwnd),
							)

							appendOCRLog(
								"AUTO ACCEPT | ENTER SENT | HWND=0x%X",
								uintptr(hwnd),
							)

						} else {

							fmt.Printf(
								"[AutoAccept] ENTER FAILED | HWND 0x%X\n",
								uintptr(hwnd),
							)

							appendOCRLog(
								"AUTO ACCEPT | ENTER FAILED | HWND=0x%X",
								uintptr(hwnd),
							)
						}
					}
				}
			}

		default:
		}

		// ====================================================
		// CURSOR
		// ====================================================

		var point POINT

		ret, _, _ :=
			getCursorPos.Call(
				uintptr(
					unsafe.Pointer(&point),
				),
			)

		if ret == 0 {

			time.Sleep(
				20 * time.Millisecond,
			)

			continue
		}

		// Diagnostic only: inspect the control focused by the game thread.
		// This tells us whether chat has a distinct native focus target before
		// using focus as the source of an automatic chat guard.
		if time.Since(lastFocusPoll) >= 500*time.Millisecond {
			lastFocusPoll = time.Now()

			focusHWND, focusClass, focusText, err := getTargetFocusInfo(hwnd)
			if err != nil {
				appendOCRLog("FOCUS INFO ERROR | %v", err)
			} else if focusHWND == 0 {
				// SDL temporarily reports no focused child for several normal game
				// interactions. It is not actionable and used to flood the log.
				lastFocusHWND = 0
				lastFocusClass = ""
				lastFocusText = ""
			} else if focusHWND != lastFocusHWND ||
				focusClass != lastFocusClass ||
				focusText != lastFocusText {

				lastFocusHWND = focusHWND
				lastFocusClass = focusClass
				lastFocusText = focusText

				appendOCRLog(
					"FOCUS INFO | HWND=0x%X | Class=%q | Text=%q",
					focusHWND,
					focusClass,
					focusText,
				)
			}
		}

		// ====================================================
		// HOTKEY
		// ====================================================

		f8Down := isKeyDown(getAsyncKeyState, VK_F8)
		f9Down := isKeyDown(getAsyncKeyState, VK_F9)

		// ====================================================
		// F8
		// ====================================================

		if f8Down &&
			!previousF8 {

			mouseX :=
				int(point.X)

			mouseY :=
				int(point.Y)

			relativeX :=
				mouseX -
					int(rect.Left)

			relativeY :=
				mouseY -
					int(rect.Top)

			frameX :=
				int(
					float64(relativeX) *
						scaleX,
				)

			frameY :=
				int(
					float64(relativeY) *
						scaleY,
				)

			fmt.Println()
			fmt.Println("========================================")
			fmt.Println("MOUSE CAPTURED")
			fmt.Println("========================================")

			fmt.Printf(
				"Desktop : X=%d Y=%d\n",
				mouseX,
				mouseY,
			)

			fmt.Printf(
				"Window  : X=%d Y=%d\n",
				relativeX,
				relativeY,
			)

			fmt.Printf(
				"WGC     : X=%d Y=%d\n",
				frameX,
				frameY,
			)

			if pixel, ok :=
				reader.GetPixel(
					frameX,
					frameY,
				); ok {

				fmt.Printf(
					"RGB     : (%d,%d,%d)\n",
					pixel.R,
					pixel.G,
					pixel.B,
				)

				fmt.Printf(
					"HEX     : #%02X%02X%02X\n",
					pixel.R,
					pixel.G,
					pixel.B,
				)
			}

			roi :=
				ocrworker.LoadPartyROI()

			fmt.Println()
			fmt.Println("----------------------------------------")
			fmt.Println("CURRENT OCR REGION")
			fmt.Println("----------------------------------------")

			fmt.Printf(
				"X      = %d\n",
				roi.X,
			)

			fmt.Printf(
				"Y      = %d\n",
				roi.Y,
			)

			fmt.Printf(
				"Width  = %d\n",
				roi.Width,
			)

			fmt.Printf(
				"Height = %d\n",
				roi.Height,
			)

			fmt.Printf(
				"ReadText(%d, %d, %d, %d)\n",
				roi.X,
				roi.Y,
				roi.Width,
				roi.Height,
			)

			fmt.Println()

			time.Sleep(
				300 * time.Millisecond,
			)
		}

		// ====================================================
		// F9
		// ====================================================

		if f9Down &&
			!previousF9 {

			fmt.Println()
			fmt.Println("----------------------------------------")
			fmt.Println("RESET")
			fmt.Println("----------------------------------------")

			fmt.Println(
				"Arahkan mouse ke titik yang ingin dicek, lalu tekan F8.",
			)

			fmt.Println()

			time.Sleep(
				300 * time.Millisecond,
			)
		}

		previousF8 = f8Down
		previousF9 = f9Down
		// ====================================================
		// READ WGC HEADER
		// ====================================================

		header :=
			readHeader(
				reader.memory,
			)

		if header.Magic != magic {

			time.Sleep(
				20 * time.Millisecond,
			)

			continue
		}

		if header.FrameNumber ==
			lastFrameNumber {

			time.Sleep(
				5 * time.Millisecond,
			)

			continue
		}

		if header.FrameNumber != lastStatusFrameNumber {
			lastStatusFrameNumber = header.FrameNumber
			scanStatusBar()
		}

		if time.Since(lastDetectorRun) <
			partyDetectorInterval {

			time.Sleep(
				5 * time.Millisecond,
			)

			continue
		}

		lastFrameNumber =
			header.FrameNumber

		lastDetectorRun =
			time.Now()

		partyScannerEnabled := autoAccept != nil && autoAccept.IsEnabled()
		if partyScannerEnabled && time.Since(lastPartyPanelCheck) >= partyPanelCheckInterval {
			lastPartyPanelCheck = time.Now()
			visible, changed := partyMembership.Update(reader)
			if changed {
				if visible {
					appendOCRLog("PARTY PANEL DETECTED | Auto Party OCR paused")
				} else {
					appendOCRLog("PARTY PANEL CLEARED | Auto Party OCR resumed")
				}
			}
		}

		if partyScannerEnabled && !partyMembership.ShouldPause() {
			// ====================================================
			// VISUAL DETECTOR
			// ====================================================

			state := popupDetector.update(reader, popupROI)

			// ====================================================
			// STATE CHANGE
			// ====================================================

			if state != lastPopupState {

				if state {
					// Visual detector menjadi trigger.
					// OCR melakukan verification secara async.

					startOCR(
						header.FrameNumber,
						true,
					)
				} else {
					partyOCRVariantPhase = 0
				}

				lastPopupState =
					state
			}

			// ====================================================
			// OCR FALLBACK
			// ====================================================
			//
			// Kalau visual detector miss,
			// OCR tetap melakukan probing berkala.
			//
			// The fast/passive intervals are enforced by startOCR; no OCR
			// process overlaps another party OCR request.
			//
			// ====================================================

			if !ocrBusy {

				startOCR(
					header.FrameNumber,
					state,
				)
			}
		}

		// Death detection deliberately uses periodic OCR rather than the party
		// popup visual detector. A valid respawn phrase is required before any
		// pause occurs, so this remains safe even when the selected ROI changes
		// for unrelated game effects.
		if deathPause != nil && deathPause.IsEnabled() && !deathOCRBusy &&
			time.Since(lastDeathOCRAt) >= deathOCRMinInterval {
			deathROI := partyROIToFrame(LoadDeathROI(), scaleX, scaleY, frameOriginX, frameOriginY)
			deathROI = applyCaptureOffset(deathROI)
			if deathROI.Selected {
				img, ok := reader.ToImageRect(
					deathROI.X,
					deathROI.Y,
					deathROI.Width,
					deathROI.Height,
				)
				if ok {
					deathOCRBusy = true
					lastDeathOCRAt = time.Now()
					dialogMask := deathDialogMaskFromImage(img)
					go func(img image.Image, dialogMask deathDialogMask) {
						result, err := deathOCR.RunImage(img)
						event := deathOCREvent{err: err, dialogMask: dialogMask}
						if result != nil {
							event.text = result.Text
						}
						select {
						case deathOCRResultCh <- event:
						case <-stopCh:
						}
					}(img, dialogMask)
				}
			}
		}

		// Once the death pause has happened, the same selected Message-dialog
		// ROI is checked for the much stricter resurrection prompt. No Enter is
		// ever sent from generic death detection or a plain OK button.
		if deathPause != nil && deathPause.ShouldScanResurrection() && !resurrectionOCRBusy &&
			time.Since(lastResurrectionOCRAt) >= resurrectionOCRMinInterval {
			resurrectionROI := partyROIToFrame(LoadDeathROI(), scaleX, scaleY, frameOriginX, frameOriginY)
			resurrectionROI = applyCaptureOffset(resurrectionROI)
			if resurrectionROI.Selected {
				img, ok := reader.ToImageRect(
					resurrectionROI.X,
					resurrectionROI.Y,
					resurrectionROI.Width,
					resurrectionROI.Height,
				)
				if ok {
					resurrectionOCRBusy = true
					lastResurrectionOCRAt = time.Now()
					go func(img image.Image) {
						result, err := resurrectionOCR.RunResurrectionImage(img)
						event := resurrectionOCREvent{
							err:        err,
							foreground: resurrectionDialogLooksForeground(img),
						}
						if result != nil {
							event.text = result.Text
						}
						select {
						case resurrectionOCRResultCh <- event:
						case <-stopCh:
						}
					}(img)
				}
			}
		}

		targetMonitorNeeded := targetUntil != nil && (targetUntil.IsFilterEnabled() ||
			(emergency != nil && emergency.NeedsTargetMonitor()))
		targetMonitorInterval := targetBarInterval
		if emergency != nil && emergency.NeedsTargetMonitor() {
			// Emergency target acquisition is latency-sensitive, while this small
			// ROI is only a cheap red-bar pixel scan (no OCR unless Target Until
			// Dead's name filter is also enabled).
			targetMonitorInterval = 100 * time.Millisecond
		}
		if targetMonitorNeeded && time.Since(lastTargetBarAt) >= targetMonitorInterval {
			targetROI := partyROIToFrame(LoadTargetROI(), scaleX, scaleY, frameOriginX, frameOriginY)
			targetROI = applyCaptureOffset(targetROI)
			if targetROI.Selected {
				lastTargetBarAt = time.Now()
				img, ok := reader.ToImageRect(
					targetROI.X,
					targetROI.Y,
					targetROI.Width,
					targetROI.Height,
				)
				if ok {
					targetGeneration := bot.TargetGeneration()
					if targetGeneration != lastTargetGeneration {
						lastTargetGeneration = targetGeneration
						targetValidationRevision++
						targetIsOwnCharacter = false
						targetNameVerified = false
						targetNameValidationLogged = false
						targetValidationReadyAt = time.Now().Add(targetValidationSettleDelay)
						targetValidationStartedAt = time.Now()
					}
					percent, barFound := detectFilledBarPercent(img, isHPBarPixel)
					barVisible := barFound && percent > 1.5
					// A nearly-empty live monster bar can be too narrow for the
					// generic red-fill detector. Support Skills that are not marked
					// With Target require the whole Target HUD to disappear as well,
					// so they cannot fire an attack skill at a target with only a few
					// HP remaining. Attacker mode intentionally keeps the historical
					// red-bar-only retarget rule.
					targetPresentForSupport := barVisible
					if targetUntil.IsSupportMode() {
						targetPresentForSupport = barVisible || targetPanelNameLooksPresent(img)
					}
					if emergency != nil {
						emergency.ObserveTargetPanel(barVisible, bot.TargetActionsReady())
					}
					if targetUntil.IsFilterEnabled() {
						nameSignature := targetPanelNameSignature(img)
						if !targetNameSignatureSet {
							lastTargetNameSignature = nameSignature
							targetNameSignatureSet = true
						} else if nameSignature == lastTargetNameSignature {
							pendingTargetNameSignature = 0
							pendingTargetNameSignatureScans = 0
						} else if nameSignature == pendingTargetNameSignature {
							pendingTargetNameSignatureScans++
							if pendingTargetNameSignatureScans >= 2 {
								lastTargetNameSignature = nameSignature
								pendingTargetNameSignature = 0
								pendingTargetNameSignatureScans = 0
								targetValidationRevision++
								targetIsOwnCharacter = false
								targetNameVerified = false
								targetNameValidationLogged = false
								targetValidationStartedAt = time.Now()
								bot.SetTargetActionReady(false)
							}
						} else {
							pendingTargetNameSignature = nameSignature
							pendingTargetNameSignatureScans = 1
						}
						if barVisible && !targetNameVerified && targetValidationStartedAt.IsZero() {
							targetValidationStartedAt = time.Now()
						}
						if barVisible && !targetNameVerified && !targetNameOCRBusy &&
							targetUntil.ExcludedNames() != "" &&
							time.Now().After(targetValidationReadyAt) &&
							time.Since(lastTargetNameOCRAt) >= targetNameOCRMinInterval {
							targetNameOCRBusy = true
							lastTargetNameOCRAt = time.Now()
							go func(img image.Image, generation, revision uint64) {
								result, err := targetNameOCR.RunTargetNameImage(targetPanelNameImage(img))
								event := targetNameOCREvent{generation: generation, revision: revision, err: err}
								if result != nil {
									event.text = result.Text
								}
								select {
								case targetNameOCRResultCh <- event:
								case <-stopCh:
								}
							}(img, targetGeneration, targetValidationRevision)
						}
						if barVisible && !targetNameVerified && !targetNameOCRBusy && !targetValidationStartedAt.IsZero() &&
							time.Since(targetValidationStartedAt) >= targetValidationTimeout &&
							targetUntil.ForceRetarget() {
							bot.CastTarget()
							appendOCRLog("TARGET VALIDATION | Name unavailable | Target sent")
						}
						if !barVisible {
							targetIsOwnCharacter = false
							targetNameVerified = false
							bot.SetTargetActionReady(false)
						}
						retarget := targetUntil.Observe(targetPresentForSupport)
						// Support-only slots that can start an attack are released only
						// after the same six-frame target-clear confirmation used before
						// retargeting. Attacker mode always reports false here and keeps
						// the existing immediate E behaviour unchanged.
						targetClearConfirmed := targetUntil.TargetClearConfirmed()
						bot.SetTargetPanelClear(targetClearConfirmed)
						if targetUntil.IsSupportMode() {
							retarget = targetUntil.RetargetAfterSupportDispatch(
								bot.supportTargetClearDispatchComplete(),
							)
						}
						if !targetIsOwnCharacter && retarget {
							// RetargetAfterSupportDispatch has consumed the one confirmed
							// target-free window. Clear this flag *before* E, not on the
							// following WGC scan: otherwise a skill timer that expires in
							// that short gap can see "clear" after a new, full-HP monster
							// has already been selected and leak an attack-causing skill.
							bot.SetTargetPanelClear(false)
							bot.CastTarget()
						}
					}
				}
			}
		}

		time.Sleep(
			5 * time.Millisecond,
		)
	}
}

// ============================================================
// GET WINDOW RECT
// ============================================================

func getWindowRect(
	hwnd windows.Handle,
) (RECT, error) {

	user32 :=
		windows.NewLazySystemDLL(
			"user32.dll",
		)

	getClientRect := user32.NewProc("GetClientRect")
	clientToScreen := user32.NewProc("ClientToScreen")

	var client RECT
	if ret, _, err := getClientRect.Call(
		uintptr(hwnd), uintptr(unsafe.Pointer(&client)),
	); ret == 0 {
		return RECT{}, fmt.Errorf("GetClientRect failed: %v", err)
	}

	topLeft := POINT{X: client.Left, Y: client.Top}
	bottomRight := POINT{X: client.Right, Y: client.Bottom}
	if ret, _, err := clientToScreen.Call(
		uintptr(hwnd), uintptr(unsafe.Pointer(&topLeft)),
	); ret == 0 {
		return RECT{}, fmt.Errorf("ClientToScreen (top left) failed: %v", err)
	}
	if ret, _, err := clientToScreen.Call(
		uintptr(hwnd), uintptr(unsafe.Pointer(&bottomRight)),
	); ret == 0 {
		return RECT{}, fmt.Errorf("ClientToScreen (bottom right) failed: %v", err)
	}

	return RECT{
		Left:   topLeft.X,
		Top:    topLeft.Y,
		Right:  bottomRight.X,
		Bottom: bottomRight.Y,
	}, nil
}

// ============================================================
// KEY STATE
// ============================================================

func isKeyDown(
	proc *windows.LazyProc,
	key int,
) bool {

	ret, _, _ :=
		proc.Call(
			uintptr(key),
		)

	return int16(ret) < 0
}

// ============================================================
// GET PIXEL
// ============================================================

func (r *FrameReader) GetPixel(
	x int,
	y int,
) (Pixel, bool) {

	if x < 0 ||
		y < 0 ||
		x >= r.width ||
		y >= r.height {

		return Pixel{}, false
	}

	offset :=
		y*r.stride +
			x*4

	if offset+3 >= len(r.pixels) {
		return Pixel{}, false
	}

	return Pixel{
		B: r.pixels[offset+0],
		G: r.pixels[offset+1],
		R: r.pixels[offset+2],
		A: r.pixels[offset+3],
	}, true
}

// ============================================================
// TO IMAGE
// ============================================================

func (r *FrameReader) ToImage() image.Image {

	img :=
		image.NewRGBA(
			image.Rect(
				0,
				0,
				r.width,
				r.height,
			),
		)

	for y := 0; y < r.height; y++ {

		srcOffset :=
			y * r.stride

		dstOffset :=
			y * img.Stride

		if srcOffset+r.width*4 >
			len(r.pixels) {

			break
		}

		for x := 0; x < r.width; x++ {

			src :=
				srcOffset +
					x*4

			dst :=
				dstOffset +
					x*4

			// BGRA -> RGBA

			img.Pix[dst+0] =
				r.pixels[src+2]

			img.Pix[dst+1] =
				r.pixels[src+1]

			img.Pix[dst+2] =
				r.pixels[src+0]

			img.Pix[dst+3] =
				r.pixels[src+3]
		}
	}

	return img
}

// ============================================================
// TO IMAGE RECT
// ============================================================

func (r *FrameReader) ToImageRect(
	x int,
	y int,
	width int,
	height int,
) (image.Image, bool) {

	if width <= 0 ||
		height <= 0 ||
		x < 0 ||
		y < 0 ||
		x+width > r.width ||
		y+height > r.height {

		return nil, false
	}

	img :=
		image.NewRGBA(
			image.Rect(
				0,
				0,
				width,
				height,
			),
		)

	for row := 0; row < height; row++ {

		srcOffset :=
			(y+row)*r.stride +
				x*4

		dstOffset :=
			row * img.Stride

		if srcOffset+width*4 >
			len(r.pixels) {

			return nil, false
		}

		for col := 0; col < width; col++ {

			src :=
				srcOffset +
					col*4

			dst :=
				dstOffset +
					col*4

			img.Pix[dst+0] =
				r.pixels[src+2]

			img.Pix[dst+1] =
				r.pixels[src+1]

			img.Pix[dst+2] =
				r.pixels[src+0]

			img.Pix[dst+3] =
				r.pixels[src+3]
		}
	}

	return img, true
}

// partyROIToFrame converts the picker coordinates (relative to the selected
// window) into WGC frame coordinates. This keeps the selected area correct
// when Windows DPI scaling makes the captured frame a different size.
func partyROIToFrame(
	roi ocrworker.PartyROIConfig,
	scaleX float64,
	scaleY float64,
	frameOriginX int,
	frameOriginY int,
) ocrworker.PartyROIConfig {

	return ocrworker.PartyROIConfig{
		X:        frameOriginX + int(float64(roi.X)*scaleX),
		Y:        frameOriginY + int(float64(roi.Y)*scaleY),
		Width:    max(1, int(float64(roi.Width)*scaleX)),
		Height:   max(1, int(float64(roi.Height)*scaleY)),
		Selected: roi.Selected,
	}
}

func (m *RuntimeManager) setCaptureOffset(x, y int) {
	m.mu.Lock()
	if m.running {
		m.captureOffsetX = x
		m.captureOffsetY = y
	}
	m.mu.Unlock()
}

// automaticPopupROI is independent from the optional OCR text selection.
// Confirmation dialogs in the game are centered, so this region lets chat
// guard recognize party/trade/drop dialogs immediately after Start.
func automaticPopupROI(width int, height int) ocrworker.PartyROIConfig {
	return ocrworker.PartyROIConfig{
		X:      width * 3 / 10,
		Y:      height * 2 / 5,
		Width:  max(1, width*2/5),
		Height: max(1, height/4),
	}
}

// ============================================================
// SHARED MEMORY
// ============================================================

func waitForSharedMemory() (
	windows.Handle,
	error,
) {

	for {

		handle, err :=
			openSharedMemory()

		if err == nil {
			return handle, nil
		}

		fmt.Printf(
			"\rWaiting for KaToolsKathanaFrame...",
		)

		time.Sleep(
			time.Second,
		)
	}
}

// ============================================================
// OPEN FILE MAPPING
// ============================================================

func openSharedMemory() (
	windows.Handle,
	error,
) {

	kernel32 :=
		windows.NewLazySystemDLL(
			"kernel32.dll",
		)

	proc :=
		kernel32.NewProc(
			"OpenFileMappingW",
		)

	name, err :=
		windows.UTF16PtrFromString(
			sharedMemoryName,
		)

	if err != nil {
		return 0, err
	}

	const FILE_MAP_READ = 0x0004

	ret, _, callErr :=
		proc.Call(
			uintptr(FILE_MAP_READ),
			0,
			uintptr(
				unsafe.Pointer(name),
			),
		)

	if ret == 0 {

		if callErr !=
			syscall.Errno(0) {

			return 0, callErr
		}

		return 0,
			syscall.GetLastError()
	}

	return windows.Handle(ret), nil
}

// ============================================================
// READ HEADER
// ============================================================

func readHeader(
	base uintptr,
) SharedFrameHeader {

	return SharedFrameHeader{

		Magic: binary.LittleEndian.Uint32(
			readBytes(
				base+0,
				4,
			),
		),

		Width: binary.LittleEndian.Uint32(
			readBytes(
				base+4,
				4,
			),
		),

		Height: binary.LittleEndian.Uint32(
			readBytes(
				base+8,
				4,
			),
		),

		Stride: binary.LittleEndian.Uint32(
			readBytes(
				base+12,
				4,
			),
		),

		BytesPerPixel: binary.LittleEndian.Uint32(
			readBytes(
				base+16,
				4,
			),
		),

		FrameNumber: binary.LittleEndian.Uint64(
			readBytes(
				base+20,
				8,
			),
		),

		DataSize: binary.LittleEndian.Uint32(
			readBytes(
				base+28,
				4,
			),
		),

		FrameReady: int32(
			binary.LittleEndian.Uint32(
				readBytes(
					base+32,
					4,
				),
			),
		),
	}
}

// ============================================================
// RAW MEMORY
// ============================================================

func readBytes(
	address uintptr,
	size uintptr,
) []byte {

	var bytes []byte

	sliceHeader :=
		(*[3]uintptr)(
			unsafe.Pointer(&bytes),
		)

	sliceHeader[0] = address
	sliceHeader[1] = size
	sliceHeader[2] = size

	return bytes
}

// ============================================================
// WGC CAPTURE
// ============================================================

func startWGCCapture(
	hwnd uintptr,
) (*exec.Cmd, error) {

	exePath, err :=
		os.Executable()

	if err != nil {
		return nil, err
	}

	workingDir :=
		filepath.Dir(exePath)

	capturePath :=
		filepath.Join(
			workingDir,
			"important_file",
			"wgc_capture.exe",
		)

	// go run fallback.
	if _, err :=
		os.Stat(capturePath); os.IsNotExist(err) {

		currentDir, err :=
			os.Getwd()

		if err != nil {
			return nil, err
		}

		capturePath =
			filepath.Join(
				currentDir,
				"important_file",
				"wgc_capture.exe",
			)
	}

	if _, err :=
		os.Stat(capturePath); err != nil {

		return nil,
			fmt.Errorf(
				"wgc_capture.exe not found: %s: %w",
				capturePath,
				err,
			)
	}

	fmt.Println("----------------------------------------")
	fmt.Println("Starting WGC Capture")
	fmt.Println("----------------------------------------")

	fmt.Println(
		"Path:",
		capturePath,
	)

	hwndArg :=
		fmt.Sprintf(
			"0x%X",
			hwnd,
		)

	cmd :=
		exec.Command(
			capturePath,
			"--hwnd",
			hwndArg,
		)

	cmd.SysProcAttr =
		&syscall.SysProcAttr{
			HideWindow: true,
		}

	cmd.Stdout = nil
	cmd.Stderr = nil

	if err :=
		cmd.Start(); err != nil {

		return nil,
			fmt.Errorf(
				"failed to start wgc_capture.exe: %w",
				err,
			)
	}

	fmt.Printf(
		"[WGC] Started successfully. PID=%d\n",
		cmd.Process.Pid,
	)

	return cmd, nil
}

// ============================================================
// OCR LOG
// ============================================================

var (
	ocrLogMu        sync.Mutex
	ocrLogPath      string
	ocrLogStartedAt time.Time
)

const ocrLogRotationInterval = 6 * time.Hour

func getOCRLogPath() string {

	ocrLogMu.Lock()
	defer ocrLogMu.Unlock()

	if ocrLogPath != "" {
		return ocrLogPath
	}

	cwd, err :=
		os.Getwd()

	if err != nil ||
		cwd == "" {

		return "katools_ocr.log"
	}

	ocrLogPath =
		filepath.Join(
			cwd,
			"katools_ocr.log",
		)

	return ocrLogPath
}

func resetOCRLog() {

	ocrLogMu.Lock()
	defer ocrLogMu.Unlock()

	cwd, err :=
		os.Getwd()

	if err != nil ||
		cwd == "" {

		ocrLogPath =
			"katools_ocr.log"

	} else {

		ocrLogPath =
			filepath.Join(
				cwd,
				"katools_ocr.log",
			)
	}

	if err := startOCRLogLocked(ocrLogPath, time.Now()); err != nil {

		fmt.Printf(
			"[OCR LOG] RESET FAILED | Path=%s | Error=%v\n",
			ocrLogPath,
			err,
		)

		return
	}

	fmt.Printf(
		"[OCR LOG] %s\n",
		ocrLogPath,
	)
}

// startOCRLogLocked begins a fresh active log while preserving exactly one
// previous file. It is called at bot start and again every six hours, keeping
// katools_ocr.log short without silently discarding the most recent history.
func startOCRLogLocked(path string, startedAt time.Time) error {
	if err := archiveOCRLogLocked(path); err != nil {
		return err
	}

	header :=
		"========================================\n" +
			"KaTools OCR Log\n" +
			"Started: " +
			startedAt.Format("2006-01-02 15:04:05.000") +
			"\n" +
			"========================================\n\n"
	if err := os.WriteFile(path, []byte(header), 0644); err != nil {
		return err
	}
	ocrLogStartedAt = startedAt
	return nil
}

func archiveOCRLogLocked(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) || (err == nil && info.Size() == 0) {
		return nil
	}
	if err != nil {
		return err
	}

	backupPath := ocrLogBackupPath(path)
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(path, backupPath)
}

func ocrLogBackupPath(path string) string {
	extension := filepath.Ext(path)
	base := strings.TrimSuffix(path, extension)
	return base + ".previous" + extension
}

func appendOCRLog(
	format string,
	args ...interface{},
) {

	path :=
		getOCRLogPath()

	ocrLogMu.Lock()
	defer ocrLogMu.Unlock()

	now := time.Now()
	if ocrLogStartedAt.IsZero() || now.Sub(ocrLogStartedAt) >= ocrLogRotationInterval {
		if err := startOCRLogLocked(path, now); err != nil {
			fmt.Printf(
				"[OCR LOG] ROTATION FAILED | Path=%s | Error=%v\n",
				path,
				err,
			)
		}
	}

	file, err :=
		os.OpenFile(
			path,
			os.O_CREATE|
				os.O_WRONLY|
				os.O_APPEND,
			0644,
		)

	if err != nil {

		fmt.Printf(
			"[OCR LOG] WRITE FAILED | Path=%s | Error=%v\n",
			path,
			err,
		)

		return
	}

	defer file.Close()

	timestamp := now.Format("2006-01-02 15:04:05.000")

	fmt.Fprintf(
		file,
		"%s | %s\n",
		timestamp,
		fmt.Sprintf(
			format,
			args...,
		),
	)
}

func oCRTextForLog(
	text string,
) string {

	text =
		strings.ReplaceAll(
			text,
			"\r",
			" ",
		)

	text =
		strings.ReplaceAll(
			text,
			"\n",
			" ",
		)

	text =
		strings.ReplaceAll(
			text,
			"\t",
			" ",
		)

	return strings.Join(
		strings.Fields(text),
		" ",
	)
}

// resolveTesseractPath prefers a bundled portable Tesseract installation.
// For `go run`, the executable is in Go's temporary cache, so the current
// project directory is also checked. A system installation remains a fallback
// for existing users who have not copied the portable files yet.
func resolveTesseractPath() string {
	const systemTesseractPath = `C:\Program Files\Tesseract-OCR\tesseract.exe`

	baseDirs := make([]string, 0, 2)
	if executable, err := os.Executable(); err == nil && executable != "" {
		baseDirs = append(baseDirs, filepath.Dir(executable))
	}
	if workingDir, err := os.Getwd(); err == nil && workingDir != "" {
		baseDirs = append(baseDirs, workingDir)
	}

	for _, baseDir := range baseDirs {
		for _, relativePath := range []string{
			filepath.Join("important_file", "tesseract", "tesseract.exe"),
			filepath.Join("important_file", "tesseract.exe"),
		} {
			candidate := filepath.Join(baseDir, relativePath)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}

	return systemTesseractPath
}

func getGlobalRuntimeManager() *RuntimeManager {
	return globalRuntimeManager
}
