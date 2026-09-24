package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

const (
	kaToolsInternalUIAddress = "127.0.0.1:8787"
	// The web UI switches to its compact one-column layout below 750 px. Keep
	// the default side panel narrow enough for smaller laptop displays and,
	// crucially, do not set a larger Win32 minimum size than its initial width.
	// The old 960 px HintMin made a zoomed-out panel look small while Windows
	// still refused to resize the actual window any narrower.
	kaToolsPanelWidth     = 540
	kaToolsPanelMinWidth  = 420
	kaToolsPanelMinHeight = 480

	kaToolsSPIGetWorkArea = 0x0030
	kaToolsSWPNoZOrder    = 0x0004
	kaToolsSWPNoActivate  = 0x0010
)

// startWebView2UI hosts the unchanged web UI on loopback and displays it in a
// native Windows WebView2 window. It deliberately uses the shared WebView2
// Runtime installed by Windows instead of bundling a browser engine or Wails.
func startWebView2UI(runtimeManager *RuntimeManager) error {
	listener, err := net.Listen("tcp", kaToolsInternalUIAddress)
	if err != nil {
		return fmt.Errorf("start internal UI listener: %w", err)
	}

	server := &http.Server{
		Handler: newWebUIMux(runtimeManager),
	}
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			fmt.Println("KaTools internal UI server error:", serveErr)
		}
	}()

	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()

	view := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "KaTools",
			Width:  kaToolsPanelWidth,
			Height: 900,
			IconId: 1,
			Center: false,
		},
	})
	if view == nil {
		return fmt.Errorf("create WebView2 window: Microsoft Edge WebView2 Runtime is required")
	}
	defer view.Destroy()
	setKaToolsWindow(windows.Handle(uintptr(view.Window())))
	defer setKaToolsWindow(0)

	view.SetSize(kaToolsPanelMinWidth, kaToolsPanelMinHeight, webview2.HintMin)
	positionKaToolsPanelAtRight(view.Window())
	view.Navigate("http://" + kaToolsInternalUIAddress)
	view.Run()
	return nil
}

// positionKaToolsPanelAtRight makes the default UI behave like a tall side
// panel next to the game. SPI_GETWORKAREA excludes the taskbar, and per-monitor
// DPI awareness is configured before this function is called from main.
func positionKaToolsPanelAtRight(window unsafe.Pointer) {
	if window == nil {
		return
	}

	user32 := windows.NewLazySystemDLL("user32.dll")
	getWorkArea := user32.NewProc("SystemParametersInfoW")
	setWindowPos := user32.NewProc("SetWindowPos")

	var workArea RECT
	ok, _, _ := getWorkArea.Call(
		kaToolsSPIGetWorkArea,
		0,
		uintptr(unsafe.Pointer(&workArea)),
		0,
	)
	if ok == 0 {
		return
	}

	workWidth := workArea.Right - workArea.Left
	workHeight := workArea.Bottom - workArea.Top
	if workWidth <= 0 || workHeight <= 0 {
		return
	}

	panelWidth := int32(kaToolsPanelWidth)
	if panelWidth > workWidth {
		panelWidth = workWidth
	}

	_, _, _ = setWindowPos.Call(
		uintptr(window),
		0,
		uintptr(int32(workArea.Right-panelWidth)),
		uintptr(workArea.Top),
		uintptr(panelWidth),
		uintptr(workHeight),
		kaToolsSWPNoZOrder|kaToolsSWPNoActivate,
	)
}
