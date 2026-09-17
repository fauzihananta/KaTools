package main

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "image/jpeg"
)

const tesseractPath = `C:\Program Files\Tesseract-OCR\tesseract.exe`

const (
	defaultCropX = 750
	defaultCropY = 670
	defaultCropW = 350
	defaultCropH = 65

	gridSize = 50
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	imagePath := os.Args[1]

	if _, err := os.Stat(imagePath); err != nil {
		fmt.Printf("Image not found: %s\n", imagePath)
		os.Exit(1)
	}

	// ============================================================
	// GRID MODE
	// ============================================================

	if len(os.Args) >= 3 && os.Args[2] == "--grid" {
		runGridMode(imagePath)
		return
	}

	// ============================================================
	// CROP COORDINATES
	// ============================================================

	cropX := defaultCropX
	cropY := defaultCropY
	cropW := defaultCropW
	cropH := defaultCropH

	if len(os.Args) >= 6 {
		var err error

		cropX, err = strconv.Atoi(os.Args[2])
		if err != nil {
			fmt.Println("Invalid X:", os.Args[2])
			os.Exit(1)
		}

		cropY, err = strconv.Atoi(os.Args[3])
		if err != nil {
			fmt.Println("Invalid Y:", os.Args[3])
			os.Exit(1)
		}

		cropW, err = strconv.Atoi(os.Args[4])
		if err != nil {
			fmt.Println("Invalid W:", os.Args[4])
			os.Exit(1)
		}

		cropH, err = strconv.Atoi(os.Args[5])
		if err != nil {
			fmt.Println("Invalid H:", os.Args[5])
			os.Exit(1)
		}
	}

	fmt.Println("========================================")
	fmt.Println(" KaTools OCR Probe")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("Image :", imagePath)
	fmt.Println("OCR   :", tesseractPath)
	fmt.Println()

	// ============================================================
	// TEST 1 - FULL SCREEN OCR
	// ============================================================

	fmt.Println("========================================")
	fmt.Println(" TEST 1: FULL SCREEN OCR")
	fmt.Println("========================================")
	fmt.Println()

	start := time.Now()

	fullText, err := runOCR(imagePath, "11")
	fullDuration := time.Since(start)

	if err != nil {
		fmt.Println("OCR ERROR:", err)
		os.Exit(1)
	}

	fmt.Printf("OCR TIME: %v\n", fullDuration)

	fmt.Println()
	fmt.Println("----------------------------------------")
	fmt.Println("OCR RESULT")
	fmt.Println("----------------------------------------")
	fmt.Println(fullText)

	if isPartyInvite(fullText) {
		fmt.Println("PARTY DETECTED: TRUE")
	} else {
		fmt.Println("PARTY DETECTED: FALSE")
	}

	fmt.Println()

	// ============================================================
	// CREATE CROPPED IMAGE
	// ============================================================

	fmt.Println("========================================")
	fmt.Println(" TEST 2: CROPPED POPUP OCR")
	fmt.Println("========================================")
	fmt.Println()

	croppedPath := filepath.Join(
		os.TempDir(),
		"katools_ocr_popup.png",
	)

	fmt.Printf(
		"CROP    : X=%d Y=%d W=%d H=%d\n",
		cropX,
		cropY,
		cropW,
		cropH,
	)

	err = cropImage(
		imagePath,
		croppedPath,
		cropX,
		cropY,
		cropW,
		cropH,
	)

	if err != nil {
		fmt.Println("CROP ERROR:", err)
		os.Exit(1)
	}

	fmt.Println("IMAGE   :", croppedPath)

	if err := openImage(croppedPath); err != nil {
		fmt.Println("WARNING: Could not open crop:", err)
	}

	fmt.Println()

	// ============================================================
	// PSM 11
	// ============================================================

	fmt.Println("----------------------------------------")
	fmt.Println("PSM 11")
	fmt.Println("----------------------------------------")

	start = time.Now()

	psm11Text, err := runOCR(croppedPath, "11")
	psm11Duration := time.Since(start)

	if err != nil {
		fmt.Println("OCR ERROR:", err)
		os.Exit(1)
	}

	fmt.Printf("OCR TIME: %v\n", psm11Duration)

	fmt.Println()
	fmt.Println("OCR RESULT")
	fmt.Println("----------------------------------------")
	fmt.Println(psm11Text)

	psm11Party := isPartyInvite(psm11Text)

	if psm11Party {
		fmt.Println("PARTY DETECTED: TRUE")
	} else {
		fmt.Println("PARTY DETECTED: FALSE")
	}

	fmt.Println()

	// ============================================================
	// PSM 7
	// ============================================================

	fmt.Println("----------------------------------------")
	fmt.Println("PSM 7")
	fmt.Println("----------------------------------------")

	start = time.Now()

	psm7Text, err := runOCR(croppedPath, "7")
	psm7Duration := time.Since(start)

	if err != nil {
		fmt.Println("OCR ERROR:", err)
		os.Exit(1)
	}

	fmt.Printf("OCR TIME: %v\n", psm7Duration)

	fmt.Println()
	fmt.Println("OCR RESULT")
	fmt.Println("----------------------------------------")
	fmt.Println(psm7Text)

	psm7Party := isPartyInvite(psm7Text)

	if psm7Party {
		fmt.Println("PARTY DETECTED: TRUE")
	} else {
		fmt.Println("PARTY DETECTED: FALSE")
	}

	// ============================================================
	// BENCHMARK 10X - PSM 11
	// ============================================================

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println(" BENCHMARK: OCR 10X")
	fmt.Println("========================================")
	fmt.Println()

	const benchmarkRuns = 10

	var totalBenchmark time.Duration
	var minBenchmark time.Duration
	var maxBenchmark time.Duration

	for i := 1; i <= benchmarkRuns; i++ {
		start = time.Now()

		text, err := runOCR(croppedPath, "7")
		duration := time.Since(start)

		if err != nil {
			fmt.Printf("Run %02d ERROR: %v\n", i, err)
			continue
		}

		totalBenchmark += duration

		if minBenchmark == 0 || duration < minBenchmark {
			minBenchmark = duration
		}

		if duration > maxBenchmark {
			maxBenchmark = duration
		}

		detected := isPartyInvite(text)

		fmt.Printf(
			"Run %02d : %v | Party=%v\n",
			i,
			duration,
			detected,
		)
	}

	averageBenchmark := totalBenchmark / benchmarkRuns

	fmt.Println()
	fmt.Println("----------------------------------------")
	fmt.Println("BENCHMARK RESULT")
	fmt.Println("----------------------------------------")

	fmt.Printf("Runs    : %d\n", benchmarkRuns)
	fmt.Printf("Min     : %v\n", minBenchmark)
	fmt.Printf("Max     : %v\n", maxBenchmark)
	fmt.Printf("Average : %v\n", averageBenchmark)

	// ============================================================
	// COMPARISON
	// ============================================================

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println(" COMPARISON")
	fmt.Println("========================================")

	fmt.Printf(
		"Full screen PSM 11 : %v\n",
		fullDuration,
	)

	fmt.Printf(
		"Popup PSM 11       : %v | Party=%v\n",
		psm11Duration,
		psm11Party,
	)

	fmt.Printf(
		"Popup PSM 7        : %v | Party=%v\n",
		psm7Duration,
		psm7Party,
	)

	if psm7Duration > 0 {
		fmt.Printf(
			"PSM 7 speedup vs PSM 11: %.2fx\n",
			float64(psm11Duration)/float64(psm7Duration),
		)
	}

	if psm11Duration > 0 {
		fmt.Printf(
			"Popup PSM 11 speedup vs full: %.2fx\n",
			float64(fullDuration)/float64(psm11Duration),
		)
	}

	if psm7Duration > 0 {
		fmt.Printf(
			"Popup PSM 7 speedup vs full: %.2fx\n",
			float64(fullDuration)/float64(psm7Duration),
		)
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println(" DONE")
	fmt.Println("========================================")
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println()
	fmt.Println(`  go run . .\party_test_ori.jpg`)
	fmt.Println()
	fmt.Println("Custom crop:")
	fmt.Println(`  go run . .\party_test_ori.jpg X Y W H`)
	fmt.Println()
	fmt.Println("Grid mode:")
	fmt.Println(`  go run . .\party_test_ori.jpg --grid`)
}

func runGridMode(imagePath string) {
	fmt.Println("========================================")
	fmt.Println(" KaTools OCR Probe - GRID MODE")
	fmt.Println("========================================")
	fmt.Println()

	file, err := os.Open(imagePath)
	if err != nil {
		fmt.Println("OPEN ERROR:", err)
		os.Exit(1)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		fmt.Println("DECODE ERROR:", err)
		os.Exit(1)
	}

	bounds := img.Bounds()

	fmt.Printf(
		"Image size : %d x %d\n",
		bounds.Dx(),
		bounds.Dy(),
	)

	fmt.Printf(
		"Grid size  : %d px\n",
		gridSize,
	)

	fmt.Println()

	gridPath := filepath.Join(
		os.TempDir(),
		"katools_ocr_grid.png",
	)

	if err := createGridImage(imagePath, gridPath); err != nil {
		fmt.Println("GRID ERROR:", err)
		os.Exit(1)
	}

	fmt.Println("GRID IMAGE :", gridPath)
	fmt.Println()

	if err := openImage(gridPath); err != nil {
		fmt.Println("WARNING: Could not open grid automatically:")
		fmt.Println(err)
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println(" DONE")
	fmt.Println("========================================")
}

func createGridImage(inputPath string, outputPath string) error {
	file, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open image: %w", err)
	}
	defer file.Close()

	src, _, err := image.Decode(file)
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}

	bounds := src.Bounds()

	canvas := image.NewRGBA(bounds)

	draw.Draw(
		canvas,
		bounds,
		src,
		bounds.Min,
		draw.Src,
	)

	for x := bounds.Min.X; x < bounds.Max.X; x += gridSize {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			canvas.Set(x, y, image.Black)
		}
	}

	for y := bounds.Min.Y; y < bounds.Max.Y; y += gridSize {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			canvas.Set(x, y, image.Black)
		}
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer out.Close()

	if err := png.Encode(out, canvas); err != nil {
		return fmt.Errorf("encode grid: %w", err)
	}

	return nil
}

func runOCR(imagePath string, psm string) (string, error) {
	cmd := exec.Command(
		tesseractPath,
		imagePath,
		"stdout",
		"--psm",
		psm,
	)

	output, err := cmd.CombinedOutput()

	if err != nil {
		return "", fmt.Errorf(
			"tesseract failed: %w\n%s",
			err,
			string(output),
		)
	}

	return string(output), nil
}

func cropImage(
	inputPath string,
	outputPath string,
	x int,
	y int,
	w int,
	h int,
) error {
	file, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open image: %w", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()

	if x < 0 ||
		y < 0 ||
		w <= 0 ||
		h <= 0 ||
		x+w > bounds.Dx() ||
		y+h > bounds.Dy() {

		return fmt.Errorf(
			"crop outside image bounds: image=%dx%d crop=(%d,%d,%d,%d)",
			bounds.Dx(),
			bounds.Dy(),
			x,
			y,
			w,
			h,
		)
	}

	cropped := image.NewRGBA(
		image.Rect(0, 0, w, h),
	)

	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			cropped.Set(
				xx,
				yy,
				img.At(x+xx, y+yy),
			)
		}
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create cropped image: %w", err)
	}
	defer out.Close()

	if err := png.Encode(out, cropped); err != nil {
		return fmt.Errorf("encode cropped image: %w", err)
	}

	return nil
}

func openImage(path string) error {
	cmd := exec.Command(
		"rundll32.exe",
		"url.dll,FileProtocolHandler",
		path,
	)

	return cmd.Start()
}

func isPartyInvite(text string) bool {
	normalized := normalizeOCR(text)

	patterns := []string{
		`has invited you to join the party`,
		`has invited you to join the part`,
	}

	for _, pattern := range patterns {
		matched, err := regexp.MatchString(pattern, normalized)
		if err == nil && matched {
			return true
		}
	}

	hasInvited := strings.Contains(normalized, "has invited you")
	joinParty := strings.Contains(normalized, "join the party")

	return hasInvited && joinParty
}

func normalizeOCR(text string) string {
	text = strings.ToLower(text)

	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\t", " ")

	text = strings.Join(strings.Fields(text), " ")

	return text
}
