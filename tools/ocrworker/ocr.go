package ocrworker

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

const (
	// PSM 11 = sparse text. Dialog game mengandung frame, tombol, dan
	// background yang ramai; mode ini membaca baris undangan sebagai teks
	// terpisah tanpa menganggap seluruh area pilihan sebagai satu paragraf.
	PartyOCRPSM  = "11"
	PartyOCRLang = "eng"

	DefaultPartyROI_X = 770
	DefaultPartyROI_Y = 670
	DefaultPartyROI_W = 350
	DefaultPartyROI_H = 80

	partyROIFile = "party_roi.json"
)

type PartyROIConfig struct {
	X        int  `json:"x"`
	Y        int  `json:"y"`
	Width    int  `json:"width"`
	Height   int  `json:"height"`
	Selected bool `json:"selected"`
}

var (
	partyROIMu       sync.RWMutex
	partyROISelected bool

	partyROI = PartyROIConfig{
		X:      DefaultPartyROI_X,
		Y:      DefaultPartyROI_Y,
		Width:  DefaultPartyROI_W,
		Height: DefaultPartyROI_H,
	}
)

type OCR struct {
	TesseractPath string
	TessdataPath  string
}

type Result struct {
	Text         string
	IsParty      bool
	Duration     time.Duration
	StatusHPText string
	StatusTPText string
}

func New(tesseractPath string) *OCR {

	LoadPartyROI()

	tessdataPath := filepath.Join(filepath.Dir(tesseractPath), "tessdata")
	if info, err := os.Stat(tessdataPath); err != nil || !info.IsDir() {
		tessdataPath = ""
	}

	return &OCR{
		TesseractPath: tesseractPath,
		TessdataPath:  tessdataPath,
	}
}

// ============================================================
// RunImage
// ============================================================

func LoadPartyROI() PartyROIConfig {

	partyROIMu.RLock()
	current := partyROI
	partyROIMu.RUnlock()

	path := filepath.Join(".", partyROIFile)

	data, err := os.ReadFile(path)

	if err != nil {

		// File belum ada.
		// Buat file baru dengan default.
		_ = SavePartyROI(current)

		return current
	}

	var cfg PartyROIConfig

	if err := json.Unmarshal(data, &cfg); err != nil {
		return current
	}

	if cfg.Width <= 0 || cfg.Height <= 0 {
		return current
	}

	// A picked ROI is a saved user choice. Keep its selected state across app
	// launches so loading a saved KaTools configuration also restores its area.

	partyROIMu.Lock()
	partyROI = cfg
	partyROISelected = cfg.Selected
	partyROIMu.Unlock()

	return cfg
}

func SavePartyROI(cfg PartyROIConfig) error {
	return savePartyROI(cfg, false)
}

// SavePickedPartyROI records coordinates and keeps Auto Accept Party selected
// across launches until the user explicitly presses RESET.
func SavePickedPartyROI(cfg PartyROIConfig) error {
	return savePartyROI(cfg, true)
}

// ResetPickedPartyROI keeps the remembered coordinates but revokes their saved
// authorization. Auto Accept must be selected again before it can run.
func ResetPickedPartyROI() error {
	partyROIMu.RLock()
	cfg := partyROI
	partyROIMu.RUnlock()
	return savePartyROI(cfg, false)
}

func savePartyROI(cfg PartyROIConfig, selected bool) error {

	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("invalid party ROI size")
	}

	cfg.Selected = selected

	data, err := json.MarshalIndent(
		cfg,
		"",
		"  ",
	)

	if err != nil {
		return err
	}

	path := filepath.Join(".", partyROIFile)

	if err := os.WriteFile(
		path,
		data,
		0644,
	); err != nil {
		return err
	}

	partyROIMu.Lock()
	partyROI = cfg
	partyROISelected = selected
	partyROIMu.Unlock()

	return nil
}

func (o *OCR) RunImage(img image.Image) (*Result, error) {
	return o.RunPartyImageVariants(img, nil)
}

// RunPartyImageVariants performs party-dialog OCR for only the requested
// preprocessing variants. A nil or empty list preserves the historical
// behavior and tries every variant. The runtime uses a one-variant fast pass
// immediately after the visual popup detector fires, then only pays for the
// remaining fallbacks if that first read is inconclusive. This keeps party
// acceptance responsive when the game is already using most of the CPU.
func (o *OCR) RunPartyImageVariants(img image.Image, variantIndexes []int) (*Result, error) {

	if img == nil {
		return nil, fmt.Errorf("image is nil")
	}

	startTotal := time.Now()

	// Callers may supply a cropped selection whose dimensions differ from
	// party_roi.json because WGC applies DPI scaling. Treat this input as the
	// final OCR area instead of cropping it a second time.
	cropped := img

	variants := buildOCRVariants(cropped)

	var allTexts []string

	if len(variantIndexes) == 0 {
		variantIndexes = make([]int, len(variants))
		for i := range variants {
			variantIndexes[i] = i
		}
	}

	for _, i := range variantIndexes {
		if i < 0 || i >= len(variants) {
			continue
		}
		variant := variants[i]

		text, err := o.runTesseractImage(
			variant,
			i,
		)

		if err != nil {
			continue
		}

		text = normalize(text)

		if text != "" {
			allTexts = append(allTexts, text)
		}

		if IsPartyInvite(text) {

			return &Result{
				Text:     text,
				IsParty:  true,
				Duration: time.Since(startTotal),
			}, nil
		}
	}

	combined := bestText(allTexts)

	return &Result{
		Text:     combined,
		IsParty:  IsPartyInvite(combined),
		Duration: time.Since(startTotal),
	}, nil
}

// RunResurrectionImage prioritizes the two stable words from Kathana's
// resurrection confirmation instead of choosing the longest text in a noisy
// frame. A full-screen event or a party list can contain far more text than
// the small Message dialog, so generic bestText is not appropriate here.
func (o *OCR) RunResurrectionImage(img image.Image) (*Result, error) {
	if img == nil {
		return nil, fmt.Errorf("image is nil")
	}

	startTotal := time.Now()
	variants := buildOCRVariants(img)
	var allTexts []string

	// PSM 6 reads the dialog body as a text block; PSM 11 remains useful when
	// parts of the dialog are obscured by effects or other UI elements.
	for _, psm := range []string{"6", PartyOCRPSM} {
		for i, variant := range variants {
			text, err := o.runTesseractImageWithOptions(variant, i, psm, "")
			if err != nil {
				continue
			}
			text = normalize(text)
			if text == "" {
				continue
			}
			if isResurrectionText(text) {
				return &Result{Text: text, Duration: time.Since(startTotal)}, nil
			}
			allTexts = append(allTexts, text)
		}
	}

	return &Result{Text: bestText(allTexts), Duration: time.Since(startTotal)}, nil
}

func isResurrectionText(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "lost prana") && strings.Contains(text, "resurrect")
}

// RunStatusImage is intentionally separate from party-dialog OCR. Status HUD
// has exactly two numeric lines (HP and TP), so limiting Tesseract to digits
// and '/' prevents a character name or surrounding HUD text from winning the
// generic "longest text" selection used for dialogs.
func (o *OCR) RunStatusImage(img image.Image) (*Result, error) {
	if img == nil {
		return nil, fmt.Errorf("image is nil")
	}

	startTotal := time.Now()
	// The first third contains the character name and level. With a digit
	// whitelist, Tesseract treats the level (for example, "Lv.7") as an extra
	// numeric line and often merges it into HP/TP. OCR only the two bar rows.
	statusNumbers := statusNumbersImage(img)
	variants := buildOCRVariants(statusNumbers)
	allTexts := make([]string, 0, len(variants)*2)
	statusRows := statusRowImages(statusNumbers)
	var hpPairText string
	var tpPairText string

	// OCR each bar row independently before trying the two-line crop. PSM 7
	// tells Tesseract that there is exactly one text line, which keeps the HP
	// and TP digits from being joined and substantially improves recognition of
	// the small '/' glyph used by the HUD.
	if len(statusRows) == 2 {
		rowVariants := [2][]image.Image{
			buildOCRVariants(statusRows[0]),
			buildOCRVariants(statusRows[1]),
		}
		for _, variantIndex := range []int{0, 2, 3} {
			if variantIndex >= len(rowVariants[0]) || variantIndex >= len(rowVariants[1]) {
				continue
			}
			hpText, hpErr := o.runTesseractImageWithOptions(
				rowVariants[0][variantIndex],
				variantIndex*2,
				"7",
				"0123456789/",
			)
			tpText, tpErr := o.runTesseractImageWithOptions(
				rowVariants[1][variantIndex],
				variantIndex*2+1,
				"7",
				"0123456789/",
			)
			if hpErr != nil || tpErr != nil {
				continue
			}

			text := strings.TrimSpace(normalize(hpText) + " " + normalize(tpText))
			if text == "" {
				continue
			}
			allTexts = append(allTexts, text)
			if hpPairText == "" {
				hpPairText = statusRowPairText(hpText)
			}
			if tpPairText == "" {
				tpPairText = statusRowPairText(tpText)
			}
			if strings.Count(text, "/") >= 2 {
				return &Result{
					Text:         text,
					Duration:     time.Since(startTotal),
					StatusHPText: hpPairText,
					StatusTPText: tpPairText,
				}, nil
			}
		}
	}

	// PSM 11 handles two separate HUD lines better than a text block. Keep PSM
	// 6 as a fallback for font/rendering combinations where it performs better.
	for _, psm := range []string{"11", "6"} {
		for i, variant := range variants {
			text, err := o.runTesseractImageWithOptions(
				variant,
				i,
				psm,
				"0123456789/",
			)
			if err != nil {
				continue
			}

			text = normalize(text)
			if text == "" {
				continue
			}

			allTexts = append(allTexts, text)
			if strings.Count(text, "/") >= 2 {
				return &Result{
					Text:         text,
					Duration:     time.Since(startTotal),
					StatusHPText: hpPairText,
					StatusTPText: tpPairText,
				}, nil
			}
		}
	}

	return &Result{
		Text:         bestText(allTexts),
		Duration:     time.Since(startTotal),
		StatusHPText: hpPairText,
		StatusTPText: tpPairText,
	}, nil
}

var statusRowPairPattern = regexp.MustCompile(`\d{1,6}\s*/\s*\d{1,6}`)

// statusRowPairText retains the first actual numeric pair from one known
// status row. HP and TP may need different preprocessing variants, so the
// caller must be able to validate them independently instead of requiring
// both values to succeed in the same Tesseract result.
func statusRowPairText(text string) string {
	return statusRowPairPattern.FindString(normalize(text))
}

// statusNumbersImage removes the character-name/level header from a selected
// HP/TP panel. The picker asks for the full panel for a stable bar detector,
// while OCR only needs the lower bar rows.
func statusNumbersImage(src image.Image) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 4 {
		return src
	}

	top := height / 3
	if top >= height-2 {
		return src
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, height-top))
	for y := top; y < height; y++ {
		for x := 0; x < width; x++ {
			dst.Set(x, y-top, src.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return dst
}

// statusRowImages separates the HP and TP rows after the panel header has
// been removed. A small overlap keeps the baseline intact when a selection is
// one or two pixels taller or shorter on another display.
func statusRowImages(src image.Image) []image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height < 8 {
		return nil
	}

	middle := height / 2
	overlap := height / 10
	if overlap < 3 {
		overlap = 3
	}
	if overlap >= middle {
		overlap = middle - 1
	}
	if overlap < 0 {
		return nil
	}

	return []image.Image{
		cropImageRows(src, 0, middle+overlap),
		cropImageRows(src, middle-overlap, height),
	}
}

func cropImageRows(src image.Image, top, bottom int) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if top < 0 {
		top = 0
	}
	if bottom > height {
		bottom = height
	}
	if bottom <= top {
		return src
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, bottom-top))
	for y := top; y < bottom; y++ {
		for x := 0; x < width; x++ {
			dst.Set(x, y-top, src.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return dst
}

// RunTargetNameImage reads one target-HUD header. Unlike a party dialog, this
// is a compact HUD block, so PSM 6 is both faster and more reliable. Using
// only the first two image variants avoids several seconds of Tesseract work
// every time the bot changes target.
func (o *OCR) RunTargetNameImage(img image.Image) (*Result, error) {
	if img == nil {
		return nil, fmt.Errorf("image is nil")
	}

	startTotal := time.Now()
	variants := buildOCRVariants(img)
	var fallback string
	for i, variant := range variants {
		if i >= 2 {
			break
		}
		text, err := o.runTesseractImageWithOptions(
			variant,
			i,
			"6",
			"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789 ",
		)
		if err != nil {
			continue
		}
		text = normalize(text)
		if text == "" {
			continue
		}
		if fallback == "" {
			fallback = text
		}
		if targetNameTextHasLetters(text) {
			return &Result{Text: text, Duration: time.Since(startTotal)}, nil
		}
	}

	return &Result{Text: fallback, Duration: time.Since(startTotal)}, nil
}

func targetNameTextHasLetters(text string) bool {
	count := 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			count++
			if count >= 3 {
				return true
			}
		}
	}
	return false
}

// ============================================================
// Prepare Party Image
// ============================================================

func preparePartyImage(img image.Image) (image.Image, error) {

	if img == nil {
		return nil, fmt.Errorf("image is nil")
	}

	roi := LoadPartyROI()

	bounds := img.Bounds()

	width := bounds.Dx()
	height := bounds.Dy()

	// Kalau image memang sudah merupakan hasil crop.
	if width == roi.Width &&
		height == roi.Height {

		return img, nil
	}

	if roi.X < 0 ||
		roi.Y < 0 ||
		roi.Width <= 0 ||
		roi.Height <= 0 {

		return nil, fmt.Errorf(
			"invalid party ROI: %+v",
			roi,
		)
	}

	if width < roi.X+roi.Width ||
		height < roi.Y+roi.Height {

		return nil, fmt.Errorf(
			"party ROI outside image: image=%dx%d roi=(%d,%d,%d,%d)",
			width,
			height,
			roi.X,
			roi.Y,
			roi.Width,
			roi.Height,
		)
	}

	return cropPartyROI(
		img,
		roi.X,
		roi.Y,
		roi.Width,
		roi.Height,
	)
}

// ============================================================
// OCR Variants
// ============================================================

func buildOCRVariants(
	src image.Image,
) []image.Image {

	gray := grayscaleImage(src)

	upscaledGray := upscale3x(gray)

	variants := []image.Image{
		upscaledGray,

		thresholdImage(
			upscaledGray,
			120,
			false,
		),

		thresholdImage(
			upscaledGray,
			160,
			false,
		),

		thresholdImage(
			upscaledGray,
			200,
			false,
		),

		thresholdImage(
			upscaledGray,
			160,
			true,
		),
	}

	return variants
}

// ============================================================
// Grayscale
// ============================================================

func grayscaleImage(
	src image.Image,
) image.Image {

	bounds := src.Bounds()

	dst := image.NewGray(
		image.Rect(
			0,
			0,
			bounds.Dx(),
			bounds.Dy(),
		),
	)

	for y := 0; y < bounds.Dy(); y++ {

		for x := 0; x < bounds.Dx(); x++ {

			r, g, b, _ :=
				src.At(
					bounds.Min.X+x,
					bounds.Min.Y+y,
				).RGBA()

			// image.Color.RGBA returns 16-bit components. Convert to 8-bit
			// before calculating luminance; direct conversion to uint8 below
			// would wrap the high bits and turn clear yellow/white game text
			// into corrupted pixels.
			gray :=
				(299*int(r>>8) +
					587*int(g>>8) +
					114*int(b>>8)) /
					1000

			if gray < 0 {
				gray = 0
			}

			if gray > 255 {
				gray = 255
			}

			dst.SetGray(
				x,
				y,
				color.Gray{
					Y: uint8(gray),
				},
			)
		}
	}

	return dst
}

// ============================================================
// Upscale 3x
// ============================================================

func upscale3x(
	src image.Image,
) image.Image {

	bounds := src.Bounds()

	w := bounds.Dx()
	h := bounds.Dy()

	dst := image.NewGray(
		image.Rect(
			0,
			0,
			w*3,
			h*3,
		),
	)

	for y := 0; y < h; y++ {

		for x := 0; x < w; x++ {

			gray := color.GrayModel.Convert(
				src.At(
					bounds.Min.X+x,
					bounds.Min.Y+y,
				),
			).(color.Gray)

			dx := x * 3
			dy := y * 3

			for yy := 0; yy < 3; yy++ {
				for xx := 0; xx < 3; xx++ {

					dst.SetGray(
						dx+xx,
						dy+yy,
						gray,
					)
				}
			}
		}
	}

	return dst
}

// ============================================================
// Threshold
// ============================================================

func thresholdImage(
	src image.Image,
	threshold uint8,
	invert bool,
) image.Image {

	bounds := src.Bounds()

	dst := image.NewGray(
		image.Rect(
			0,
			0,
			bounds.Dx(),
			bounds.Dy(),
		),
	)

	for y := 0; y < bounds.Dy(); y++ {

		for x := 0; x < bounds.Dx(); x++ {

			c := color.GrayModel.Convert(
				src.At(
					bounds.Min.X+x,
					bounds.Min.Y+y,
				),
			).(color.Gray)

			var value uint8

			if invert {

				if c.Y >= threshold {
					value = 0
				} else {
					value = 255
				}

			} else {

				if c.Y >= threshold {
					value = 255
				} else {
					value = 0
				}
			}

			dst.SetGray(
				x,
				y,
				color.Gray{
					Y: value,
				},
			)
		}
	}

	return dst
}

// ============================================================
// Tesseract
// ============================================================

func (o *OCR) runTesseractImage(
	img image.Image,
	variant int,
) (string, error) {
	return o.runTesseractImageWithOptions(
		img,
		variant,
		PartyOCRPSM,
		"",
	)
}

func (o *OCR) runTesseractImageWithOptions(
	img image.Image,
	variant int,
	psm string,
	whitelist string,
) (string, error) {

	if strings.TrimSpace(
		o.TesseractPath,
	) == "" {

		return "",
			fmt.Errorf(
				"tesseract path is empty",
			)
	}

	if _, err :=
		os.Stat(o.TesseractPath); err != nil {

		return "",
			fmt.Errorf(
				"tesseract not found: %s: %w",
				o.TesseractPath,
				err,
			)
	}

	tempFile, err :=
		os.CreateTemp(
			os.TempDir(),
			fmt.Sprintf(
				"katools_ocr_party_%d_*.png",
				variant,
			),
		)

	if err != nil {

		return "",
			fmt.Errorf(
				"create temporary PNG: %w",
				err,
			)
	}

	tempPath := tempFile.Name()

	defer os.Remove(tempPath)

	if err :=
		png.Encode(
			tempFile,
			img,
		); err != nil {

		_ = tempFile.Close()

		return "",
			fmt.Errorf(
				"encode temporary PNG: %w",
				err,
			)
	}

	if err :=
		tempFile.Close(); err != nil {

		return "",
			fmt.Errorf(
				"close temporary PNG: %w",
				err,
			)
	}

	args := []string{
		tempPath,
		"stdout",
		"--psm",
		psm,
		"-l",
		PartyOCRLang,
		"-c",
		"preserve_interword_spaces=1",
		"--dpi",
		"300",
	}
	if whitelist != "" {
		args = append(args, "-c", "tessedit_char_whitelist="+whitelist)
	}
	if o.TessdataPath != "" {
		args = append(args, "--tessdata-dir", o.TessdataPath)
	}

	cmd := exec.Command(o.TesseractPath, args...)
	// Tesseract is a console executable. KaTools invokes it repeatedly for OCR;
	// without this flag Windows briefly creates a terminal window for every
	// invocation when KaTools itself was launched without a console.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err :=
		cmd.CombinedOutput()

	if err != nil {

		return "",
			fmt.Errorf(
				"tesseract failed: %w: %s",
				err,
				strings.TrimSpace(
					string(output),
				),
			)
	}

	return string(output), nil
}

// ============================================================
// Best Text
// ============================================================

func bestText(
	texts []string,
) string {

	if len(texts) == 0 {
		return ""
	}

	best := ""

	for _, text := range texts {

		text = normalize(text)

		if len(text) > len(best) {
			best = text
		}
	}

	return best
}

// ============================================================
// Party Detection
// ============================================================

func IsPartyInvite(
	text string,
) bool {

	text = normalize(text)

	if text == "" {
		return false
	}

	strongPatterns := []string{

		"has invited you to join the party",

		"invited you to join the party",

		"has invited you join the party",

		"invited you join the party",

		"has invited you to join party",

		"invited you to join party",
	}

	for _, pattern := range strongPatterns {

		if strings.Contains(
			text,
			pattern,
		) {
			return true
		}
	}

	hasInvited :=
		strings.Contains(
			text,
			"invited",
		)

	hasYou :=
		strings.Contains(
			text,
			"you",
		)

	hasJoin :=
		strings.Contains(
			text,
			"join",
		)

	hasParty :=
		strings.Contains(
			text,
			"party",
		)

	if hasInvited &&
		hasYou &&
		hasJoin &&
		hasParty {

		return true
	}

	hasInviteLike :=
		strings.Contains(text, "invited") ||
			strings.Contains(text, "invlted") ||
			strings.Contains(text, "invite")

	hasJoinLike :=
		strings.Contains(text, "join") ||
			strings.Contains(text, "joln")

	hasPartyLike :=
		strings.Contains(text, "party") ||
			strings.Contains(text, "partv") ||
			strings.Contains(text, "part")

	hasYouLike :=
		strings.Contains(text, "you") ||
			strings.Contains(text, "ycu")

	if hasInviteLike &&
		hasJoinLike &&
		hasPartyLike &&
		hasYouLike {

		return true
	}

	return false
}

// ============================================================
// Crop
// ============================================================

func cropPartyROI(
	img image.Image,
	x,
	y,
	w,
	h int,
) (image.Image, error) {

	if img == nil {
		return nil,
			fmt.Errorf(
				"cannot crop nil image",
			)
	}

	bounds := img.Bounds()

	if x < bounds.Min.X ||
		y < bounds.Min.Y ||
		x+w > bounds.Max.X ||
		y+h > bounds.Max.Y {

		return nil,
			fmt.Errorf(
				"party ROI outside image: image=%dx%d roi=(%d,%d,%d,%d)",
				bounds.Dx(),
				bounds.Dy(),
				x,
				y,
				w,
				h,
			)
	}

	dst :=
		image.NewRGBA(
			image.Rect(
				0,
				0,
				w,
				h,
			),
		)

	for yy := 0; yy < h; yy++ {

		for xx := 0; xx < w; xx++ {

			dst.Set(
				xx,
				yy,
				img.At(
					x+xx,
					y+yy,
				),
			)
		}
	}

	return dst, nil
}

// ============================================================
// Normalize
// ============================================================

func normalize(
	text string,
) string {

	text = strings.ToLower(text)

	text = strings.ReplaceAll(
		text,
		"\r",
		" ",
	)

	text = strings.ReplaceAll(
		text,
		"\n",
		" ",
	)

	text = strings.ReplaceAll(
		text,
		"\t",
		" ",
	)

	return strings.TrimSpace(
		strings.Join(
			strings.Fields(text),
			" ",
		),
	)
}
