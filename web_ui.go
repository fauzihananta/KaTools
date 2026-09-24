package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"KaTools/tools/ocrworker"

	"golang.org/x/sys/windows"
)

//go:embed assets/katools-logo-transparent.png
var katoolsFaviconPNG []byte

const webUIHTML = `
<!DOCTYPE html>
<html>

<head>

<meta charset="UTF-8">

<title>KaTools</title>
<link rel="icon" type="image/png" href="/favicon.png">

<style>

* {
	box-sizing: border-box;
}

body {
	font-family: Arial, sans-serif;
	background: #111827;
	color: #f3f4f6;
	margin: 0;
	padding: 12px;
}

.container {
	width: 840px;
	max-width: 100%;
	margin: auto;
	background: #1f2937;
	padding: 14px 16px;
	border-radius: 10px;
}

h1 {
	margin: 0 0 8px 0;
	font-size: 22px;
}

.app-header {
	position: sticky;
	top: 0;
	z-index: 20;
	margin: -14px -16px 10px;
	padding: 14px 16px 0;
	background: #1f2937;
}

h2 {
	margin: 0 0 7px 0;
	font-size: 17px;
}

.status {
	padding: 9px 12px;
	background: #7f1d1d;
	border-radius: 8px;
	margin-bottom: 15px;
	font-size: 15px;
}

.status.running {
	background: #14532d;
}

.status.paused {
	background: #92400e;
}

.status.stopped {
	background: #7f1d1d;
}

.section {
	margin-top: 10px;
}

.window-panel {
	background: #374151;
	padding: 8px;
	border-radius: 8px;
}

.window-row {
	display: grid;
	grid-template-columns: 1fr auto;
	gap: 8px;
}

.window-select {
	width: 100%;
	padding: 7px 9px;
	border-radius: 6px;
	border: 1px solid #4b5563;
	background: #111827;
	color: white;
	font-size: 14px;
}

.refresh-button {
	width: auto;
	margin-top: 0;
	padding: 7px 11px;
	background: #4b5563;
}

.refresh-button:hover {
	background: #6b7280;
}

.refresh-button.loading {
	cursor: progress;
	opacity: 0.8;
}

.refresh-button.loading::before {
	content: "";
	display: inline-block;
	width: 11px;
	height: 11px;
	margin-right: 6px;
	vertical-align: -1px;
	border: 2px solid rgba(255, 255, 255, 0.35);
	border-top-color: #fff;
	border-radius: 50%;
	animation: refresh-spin 0.7s linear infinite;
}

@keyframes refresh-spin {
	to { transform: rotate(360deg); }
}

.window-info {
	margin-top: 8px;
	color: #9ca3af;
	font-size: 12px;
}

.runtime-status {
	margin-top: 8px;
	font-size: 13px;
	color: #d1d5db;
}

.actions-grid {
	display: grid;
	grid-template-columns: repeat(3, 1fr);
	gap: 6px;
}

.action-card {
	display: grid;
	grid-template-columns: auto 1fr 58px;
	align-items: center;
	gap: 6px;
	background: #374151;
	padding: 6px 7px;
	border-radius: 7px;
}

.action-card label {
	font-size: 14px;
}

input[type="checkbox"] {
	width: 17px;
	height: 17px;
	margin: 0;
	cursor: pointer;
}

input[type="text"].numeric-input {
	width: 100%;
	padding: 5px 7px;
	border-radius: 5px;
	border: 1px solid #4b5563;
	background: #111827;
	color: white;
	font-size: 14px;
}

.skills-grid {
	display: grid;
	grid-template-columns: repeat(5, 1fr);
	gap: 6px;
}

.skill-card {
	display: grid;
	grid-template-columns: auto auto 54px;
	align-items: center;
	gap: 5px;
	background: #374151;
	padding: 5px 6px;
	border-radius: 7px;
}

.skill-name {
	font-size: 14px;
	font-weight: bold;
	align-self: center;
}

.skill-check {
	align-self: center;
}

.skill-delay {
	width: 54px !important;
	padding: 4px 6px !important;
	font-size: 13px !important;
}

.support-skills-grid {
	grid-template-columns: repeat(2, minmax(0, 1fr));
}

.support-skill-card {
	display: grid;
	grid-template-columns: auto auto 54px minmax(0, 1fr);
	align-items: center;
	gap: 5px;
	background: #374151;
	padding: 5px 6px;
	border-radius: 7px;
}

.support-with-target {
	font-size: 12px;
	white-space: nowrap;
	color: #d1d5db;
}

.support-with-target input {
	margin-right: 4px;
}

button {
	width: 100%;
	padding: 8px;
	border: none;
	border-radius: 7px;
	color: white;
	font-size: 14px;
	font-weight: bold;
	cursor: pointer;
	margin-top: 10px;
}

button:disabled {
	opacity: 0.45;
	cursor: not-allowed;
}

button:disabled:hover {
	background: inherit;
}

.bot-button.start {
	background: #16a34a;
}

.bot-button.start:hover {
	background: #15803d;
}

.bot-button.stop {
	background: #dc2626;
}

.bot-button.stop:hover {
	background: #b91c1c;
}

.hint {
	color: #9ca3af;
	font-size: 12px;
	margin-top: 8px;
	text-align: center;
}

.config-actions {
	display: grid;
	grid-template-columns: 1fr 1fr;
	gap: 8px;
	margin-top: 8px;
}

.config-actions button {
	margin-top: 0;
	background: #4b5563;
}

.config-actions button:hover {
	background: #6b7280;
}

.config-profile-modal {
	position: fixed;
	inset: 0;
	z-index: 100;
	display: grid;
	place-items: center;
	padding: 18px;
	background: rgba(3, 7, 18, 0.72);
}

.config-profile-modal[hidden] {
	display: none;
}

.config-profile-dialog {
	width: min(100%, 360px);
	padding: 18px;
	border: 1px solid #4b5563;
	border-radius: 10px;
	background: #1f2937;
	box-shadow: 0 20px 45px rgba(0, 0, 0, 0.45);
}

.config-profile-dialog h2 {
	margin-bottom: 6px;
}

.config-profile-dialog p {
	margin: 0 0 14px;
	color: #9ca3af;
	font-size: 13px;
}

.config-profile-dialog input,
.config-profile-dialog select {
	width: 100%;
	padding: 9px 10px;
	border: 1px solid #4b5563;
	border-radius: 6px;
	background: #111827;
	color: white;
	font-size: 14px;
}

.config-profile-dialog-actions {
	display: grid;
	grid-template-columns: 1fr 1fr;
	gap: 8px;
	margin-top: 14px;
}

.config-profile-dialog-actions button {
	margin: 0;
	background: #4b5563;
}

.config-profile-dialog-actions button:last-child {
	background: #16a34a;
}

@media (max-width: 750px) {

	.actions-grid {
		grid-template-columns: 1fr;
	}

	.skills-grid {
		grid-template-columns: repeat(2, 1fr);
	}

	.support-skills-grid {
		grid-template-columns: 1fr;
	}

	.pot-grid {
		grid-template-columns: 1fr;
	}

	.container {
		width: 100%;
	}

	.app-header {
		margin-left: -14px;
		margin-right: -14px;
		padding-left: 14px;
		padding-right: 14px;
	}

}

@media (max-width: 460px) {

	body {
		padding: 10px;
	}

	.container {
		padding: 14px;
	}

	.window-row {
		grid-template-columns: 1fr;
	}

	.refresh-button {
		width: 100%;
	}

}

.party-action-card {
	grid-template-columns: auto 1fr auto;
}

.target-name-card {
	display: grid;
	grid-template-columns: auto minmax(120px, 1fr);
	align-items: center;
	gap: 6px;
	background: #374151;
	padding: 6px 7px;
	border-radius: 7px;
}

.target-mode-card {
	grid-column: 1 / -1;
	background: #374151;
	padding: 7px 8px;
	border-radius: 7px;
}

.target-mode-title {
	font-size: 14px;
	font-weight: 700;
	margin-bottom: 6px;
}

.target-mode-options {
	display: flex;
	flex-wrap: wrap;
	gap: 6px;
}

.target-mode-option {
	display: flex;
	align-items: center;
	gap: 5px;
	padding: 5px 7px;
	border: 1px solid #4b5563;
	border-radius: 6px;
	font-size: 13px;
	cursor: pointer;
}

/* The normal Target-only "Without name" option is hidden for Target Until
   Dead. This explicit rule is needed because .target-mode-option uses
   display:flex, which otherwise overrides the browser's default [hidden]. */
.target-mode-option[hidden] {
	display: none;
}

.target-mode-settings {
	display: grid;
	grid-template-columns: minmax(150px, 1fr) auto;
	align-items: center;
	gap: 6px;
	margin-top: 7px;
}

.target-mode-settings[hidden] {
	display: none;
}

.target-mode-settings .target-name-card {
	grid-column: 1 / -1;
}

.target-name-card label {
	font-size: 13px;
}

.target-name-card input {
	width: 100%;
	min-width: 0;
	padding: 5px 7px;
	border: 1px solid #4b5563;
	border-radius: 5px;
	background: #111827;
	color: white;
	font-size: 13px;
}

.party-picker-button {
	min-width: 72px;
	height: 30px;
	padding: 0 9px;
	margin: 0;
	background: #4b5563;
	border-radius: 6px;
	font-size: 11px;
	font-weight: 700;
	line-height: 1;
}

.party-picker-button:hover {
	background: #6b7280;
}

.roi-button-group {
	display: flex;
	gap: 6px;
}

.party-action-card:has(.roi-button-group) {
	grid-template-columns: auto 1fr auto;
}

.roi-reset-button {
	min-width: 58px;
	background: #4b5563;
}

.roi-reset-button:hover {
	background: #7f1d1d;
}

.party-roi-info {
	margin-top: 7px;
	color: #9ca3af;
	font-size: 11px;
}

.party-roi-preview {
	display: none;
	margin-top: 10px;
	max-width: 100%;
	max-height: 180px;
	border: 1px solid #4b5563;
	border-radius: 6px;
}

.roi-picker-row {
	display: grid;
	grid-template-columns: 1fr auto;
	align-items: center;
	gap: 8px;
	background: #374151;
	padding: 7px 8px;
	border-radius: 7px;
}

.roi-picker-title {
	font-size: 14px;
}

.roi-picker-row .party-roi-info {
	margin-top: 3px;
}

.pot-grid {
	display: grid;
	grid-template-columns: repeat(2, 1fr);
	gap: 6px;
}

.pot-card {
	display: grid;
	grid-template-columns: auto 1fr 80px 100px;
	align-items: center;
	gap: 6px;
	background: #374151;
	padding: 7px 8px;
	border-radius: 7px;
}

.pot-card select {
	width: 100%;
	padding: 5px 7px;
	border-radius: 5px;
	border: 1px solid #4b5563;
	background: #111827;
	color: white;
}

.emergency-grid {
	display: grid;
	grid-template-columns: repeat(3, 1fr);
	gap: 6px;
}

.emergency-card {
	display: grid;
	grid-template-columns: auto minmax(72px, 1fr) auto;
	align-items: center;
	gap: 5px;
	background: #374151;
	padding: 6px 7px;
	border-radius: 7px;
}

.emergency-card select {
	width: 100%;
	min-width: 0;
	padding: 5px 7px;
	border-radius: 5px;
	border: 1px solid #4b5563;
	background: #111827;
	color: white;
}

.emergency-target-label,
.emergency-panic-row {
	display: flex;
	align-items: center;
	gap: 5px;
	font-size: 11px;
	white-space: nowrap;
	color: #d1d5db;
}

.emergency-panic-row {
	margin-top: 8px;
	background: #374151;
	padding: 7px 8px;
	border-radius: 7px;
	font-size: 13px;
}

.tab-bar {
	display: flex;
	gap: 6px;
	margin: 0 0 10px;
	border-bottom: 1px solid #4b5563;
}

.tab-button {
	border: 0;
	border-radius: 7px 7px 0 0;
	padding: 6px 11px;
	background: transparent;
	color: #9ca3af;
	font-weight: 700;
	cursor: pointer;
}

.tab-button:hover,
.tab-button.active {
	background: #374151;
	color: #f9fafb;
}

.tab-panel {
	display: none;
}

.tab-panel.active {
	display: block;
}

/* This comes after .pot-grid's desktop rule so it also works when Firefox
   uses a zoom level that makes the visible window narrower than its CSS size. */
@media (max-width: 900px) {

	.pot-grid {
		grid-template-columns: 1fr;
	}

	.emergency-grid {
		grid-template-columns: repeat(2, 1fr);
	}

}

</style>

</head>

<body>

<div class="container">

	<div class="app-header">
		<h1 id="appTitle">KaTools</h1>

		<div class="tab-bar" role="tablist" aria-label="KaTools panels">
			<button type="button" class="tab-button active" data-tab="main" onclick="showTab('main')">Bot</button>
			<button type="button" class="tab-button" data-tab="emergency" onclick="showTab('emergency')">Emergency Skill</button>
		</div>
	</div>

	<div id="mainTab" class="tab-panel active">

	<div class="section">

		<h2>Target Window</h2>

		<div class="window-panel">

			<div class="window-row">

				<select
					id="windowSelect"
					class="window-select">

					<option value="">
						Loading windows...
					</option>

				</select>

				<button
					id="refreshWindowsButton"
					class="refresh-button"
					onclick="loadWindows()">

					REFRESH

				</button>

			</div>

			<div
				id="windowInfo"
				class="window-info">

				Select the game window before pressing START.

			</div>

		</div>

	</div>

	<div class="section">

		<h2>Target Panel Area</h2>

		<div class="roi-picker-row">
			<div>
				<div class="roi-picker-title">Target panel area (shared)</div>
				<div id="targetROIInfo" class="party-roi-info">Select the target name and red HP bar first</div>
				<div class="hint">Used by Target Until Dead and Emergency skills that need a target.</div>
			</div>
			<button type="button" class="party-picker-button" onclick="openTargetPicker()" title="Select the current target name and red HP bar">SET AREA</button>
		</div>

		<img id="targetROIPreview" class="party-roi-preview" alt="Selected target name and HP bar preview">

	</div>

	<div
		id="status"
		class="status">

		● CHECKING...

	</div>

	<div class="section">

		<h2>Actions</h2>

		<div class="actions-grid">

			<div class="action-card party-action-card">

				<input
					type="checkbox"
					id="autoAccept"
					disabled>

				<label for="autoAccept">
					Auto Accept Party
				</label>

				<button
					type="button"
					class="party-picker-button"
					onclick="openPartyPicker()"
					title="Select Party OCR area">

					⋮

				</button>

			</div>

			<div class="action-card party-action-card">

				<input
					type="checkbox"
					id="autoPauseDeath"
					disabled>

				<label for="autoPauseDeath">
					Auto Pause on Death
				</label>

				<button
					type="button"
					class="party-picker-button"
					onclick="openDeathPicker()"
					title="Select full death dialog area">

					&#8942;

				</button>

			</div>

			<div class="action-card party-action-card">

				<input
					type="checkbox"
					id="autoResurrect"
					disabled>

				<label for="autoResurrect">
					Auto Resu
				</label>

			</div>

		</div>

		<div
			id="partyROIInfo"
			class="party-roi-info">

			Party OCR ROI: loading...

		</div>

		<img
			id="partyROIPreview"
			class="party-roi-preview"
			alt="Selected Party OCR area preview">

		<div
			id="deathROIInfo"
			class="party-roi-info">

			Death dialog ROI: select the full Message dialog first

		</div>

		<img
			id="deathROIPreview"
			class="party-roi-preview"
			alt="Selected death dialog area preview">

		<div class="section">
			<h2>Auto Potion</h2>

			<div class="roi-picker-row">
				<div>
					<div class="roi-picker-title">Status HP / TP area</div>
					<div id="statusROIInfo" class="party-roi-info">Select HP and TP bars first</div>
					<div id="statusReadInfo" class="party-roi-info"></div>
				</div>
				<button type="button" class="party-picker-button" onclick="openStatusPicker()" title="Select the combined HP and TP area">&#8942;</button>
			</div>

			<img id="statusROIPreview" class="party-roi-preview" alt="Selected HP and TP status area preview">

			<div class="hint auto-pot-threshold-hint">
				Threshold is a percentage (1–100). Example: HP <strong>70</strong> uses HP Pot at 70% or below; TP <strong>30</strong> uses TP Pot at 30% or below.
			</div>

			<div class="pot-grid">
				<div class="pot-card">
					<input type="checkbox" id="autoPotHP" disabled>
					<label for="autoPotHP">HP Pot</label>
					<input type="text" inputmode="decimal" class="numeric-input" id="autoPotHPPercent" value="0" placeholder="%">
					<select id="autoPotHPSlot"><option value="49">1</option><option value="50">2</option><option value="51">3</option><option value="52">4</option><option value="53">5</option><option value="54">6</option><option value="55">7</option><option value="56">8</option><option value="57">9</option><option value="48">0</option><option value="112">F1</option><option value="113">F2</option><option value="114">F3</option><option value="115">F4</option><option value="116">F5</option><option value="117">F6</option><option value="118">F7</option><option value="119">F8</option><option value="120">F9</option><option value="121">F10</option></select>
				</div>
				<div class="pot-card">
					<input type="checkbox" id="autoPotTP" disabled>
					<label for="autoPotTP">TP Pot</label>
					<input type="text" inputmode="decimal" class="numeric-input" id="autoPotTPPercent" value="0" placeholder="%">
					<select id="autoPotTPSlot"><option value="49">1</option><option value="50">2</option><option value="51">3</option><option value="52">4</option><option value="53">5</option><option value="54">6</option><option value="55">7</option><option value="56">8</option><option value="57">9</option><option value="48">0</option><option value="112">F1</option><option value="113">F2</option><option value="114">F3</option><option value="115">F4</option><option value="116">F5</option><option value="117">F6</option><option value="118">F7</option><option value="119">F8</option><option value="120">F9</option><option value="121">F10</option></select>
				</div>
			</div>

		</div>

		<div class="actions-grid">

			<div class="target-mode-card">
				<div class="target-mode-title">Target Mode</div>
				<div class="target-mode-options">
					<label class="target-mode-option"><input type="radio" name="targetMode" value="assist" checked> Assist / Off</label>
					<label class="target-mode-option"><input type="radio" name="targetMode" value="normal"> Target</label>
					<label class="target-mode-option"><input type="radio" name="targetMode" value="until" id="targetUntilDead"> Target Until Dead</label>
				</div>

				<div id="targetAssistSettings" class="target-mode-settings">
					<label for="assistSkillSlot">Assist skill</label>
					<select id="assistSkillSlot" title="Leave on Off to disable Assist skill spam"><option value="0">Off</option><option value="49">1</option><option value="50">2</option><option value="51">3</option><option value="52">4</option><option value="53">5</option><option value="54">6</option><option value="55">7</option><option value="56">8</option><option value="57">9</option><option value="48">0</option><option value="112">F1</option><option value="113">F2</option><option value="114">F3</option><option value="115">F4</option><option value="116">F5</option><option value="117">F6</option><option value="118">F7</option><option value="119">F8</option><option value="120">F9</option><option value="121">F10</option></select>
					<label for="assistSkillDelay">Interval (seconds)</label>
					<input type="text" inputmode="decimal" class="numeric-input" id="assistSkillDelay" value="1" step="0.1" min="0.1">
				</div>

				<div id="targetNormalSettings" class="target-mode-settings" hidden>
					<label for="targetDelay">Target interval (seconds)</label>
					<input type="text" inputmode="decimal" class="numeric-input" id="targetDelay" value="0" step="0.1" min="0.1">
				</div>

				<div id="targetSkipNamesSettings" class="target-mode-settings" hidden>
					<div class="target-name-card">
						<label>Target Name Filter</label>
					<div class="target-mode-options">
						<label class="target-mode-option" id="targetWithoutNameOption"><input type="radio" name="targetNameFilterMode" value="none"> Without name</label>
						<label class="target-mode-option"><input type="radio" name="targetNameFilterMode" value="skip" checked> Skip listed</label>
						<label class="target-mode-option"><input type="radio" name="targetNameFilterMode" value="whitelist"> Only attack listed</label>
						</div>
						<label id="targetNameListLabel" for="targetUntilDeadCharacterName">Skip Target Names</label>
						<input type="text" id="targetUntilDeadCharacterName" placeholder="Example: MangAep;Dadati" autocomplete="off">
					</div>
					<div id="targetNameListHint" class="hint">Optional for Target. Required for Target Until Dead. Separate names with a semicolon.</div>
				</div>

				<div id="targetUntilSettings" class="target-mode-settings" hidden>
					<div class="hint">Uses the shared Target Panel Area above.</div>
					<div class="target-name-card">
						<label>Character Role</label>
						<div class="target-mode-options">
							<label class="target-mode-option"><input type="radio" name="targetUntilRole" value="attacker" checked> Attacker</label>
							<label class="target-mode-option"><input type="radio" name="targetUntilRole" value="support"> Support</label>
						</div>
					</div>
					<div id="targetSupportSkills" hidden>
						<div class="hint">Support Skills 1–0 replace the normal Skill scheduler in this mode. With Target may cast while a monster target is active; unchecked slots wait until the confirmed Target HUD is gone.</div>
						<div id="supportSkills" class="skills-grid support-skills-grid"></div>
					</div>
				</div>
			</div>

			<div class="action-card">

				<input
					type="checkbox"
					id="attack">

				<label for="attack">
					Attack
				</label>

				<input
					type="text"
					inputmode="decimal"
					class="numeric-input"
					id="attackDelay"
					value="0"
					step="0.1"
					min="0.1">

			</div>

			<div class="action-card">

				<input
					type="checkbox"
					id="pick">

				<label for="pick">
					Pick
				</label>

				<input
					type="text"
					inputmode="decimal"
					class="numeric-input"
					id="pickDelay"
					value="0"
					step="0.1"
					min="0.1">

			</div>

		</div>

	</div>

	<div class="section" id="normalNumberSkillsSection">

		<h2>Skills — Number</h2>

		<div class="skills-grid">

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill1">
				<label class="skill-name">1</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay1" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill2">
				<label class="skill-name">2</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay2" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill3">
				<label class="skill-name">3</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay3" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill4">
				<label class="skill-name">4</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay4" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill5">
				<label class="skill-name">5</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay5" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill6">
				<label class="skill-name">6</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay6" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill7">
				<label class="skill-name">7</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay7" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill8">
				<label class="skill-name">8</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay8" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill9">
				<label class="skill-name">9</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay9" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill0">
				<label class="skill-name">0</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay0" value="0">
			</div>

		</div>

	</div>

	<div class="section" id="normalFunctionSkillsSection">

		<h2>Skills — Function</h2>

		<div class="skills-grid">

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF1">
				<label class="skill-name">F1</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF1" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF2">
				<label class="skill-name">F2</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF2" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF3">
				<label class="skill-name">F3</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF3" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF4">
				<label class="skill-name">F4</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF4" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF5">
				<label class="skill-name">F5</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF5" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF6">
				<label class="skill-name">F6</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF6" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF7">
				<label class="skill-name">F7</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF7" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF8">
				<label class="skill-name">F8</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF8" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF9">
				<label class="skill-name">F9</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF9" value="0">
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF10">
				<label class="skill-name">F10</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF10" value="0">
			</div>

		</div>

	</div>

	<button
		id="botButton"
		class="bot-button start"
		onclick="toggleBot()">

		START BOT

	</button>

	<div class="config-actions">
		<button type="button" onclick="openSaveConfigDialog()">SAVE CONFIG</button>
		<button type="button" onclick="openLoadConfigDialog()">LOAD CONFIG</button>
	</div>

	<div class="hint">
		Delays are in seconds. Config is saved only when SAVE CONFIG is pressed.
	</div>
	</div>

	<div id="configProfileModal" class="config-profile-modal" hidden>
		<div class="config-profile-dialog" role="dialog" aria-modal="true" aria-labelledby="configProfileTitle">
			<h2 id="configProfileTitle">Save Character Config</h2>
			<p id="configProfileHint">Enter the character name for this config.</p>
			<input id="configProfileName" type="text" maxlength="48" autocomplete="off" placeholder="Character name">
			<select id="configProfileList" hidden></select>
			<div class="config-profile-dialog-actions">
				<button type="button" onclick="closeConfigProfileDialog()">CANCEL</button>
				<button id="configProfileConfirm" type="button" onclick="confirmConfigProfileDialog()">SAVE</button>
			</div>
		</div>
	</div>

	<div id="emergencyTab" class="tab-panel">
		<div class="section">
			<h2>Emergency Skill</h2>
			<div class="hint" id="emergencyTargetHint">Skills without Need Target are sent with HP Pot. Target skills wait for the Target panel.</div>
			<div id="emergencySkills" class="emergency-grid"></div>
			<label class="emergency-panic-row" for="assistPanicTarget">
				<input type="checkbox" id="assistPanicTarget">
				Assist Panic Target — when assisting with no active target, send E once only while HP is low.
			</label>
			<div class="hint">Available only when HP Pot is enabled. Emergency actions stop as soon as HP is above its configured threshold.</div>
		</div>
	</div>

</div>


<script>

const emergencySkillOptions = [
	["49", "1"], ["50", "2"], ["51", "3"], ["52", "4"], ["53", "5"],
	["54", "6"], ["55", "7"], ["56", "8"], ["57", "9"], ["48", "0"],
	["112", "F1"], ["113", "F2"], ["114", "F3"], ["115", "F4"], ["116", "F5"],
	["117", "F6"], ["118", "F7"], ["119", "F8"], ["120", "F9"], ["121", "F10"]
];

let botRuntimeActive = false;
let targetROIReady = false;
let windowsLoading = false;

function showTab(tabName) {
	for (const panel of document.querySelectorAll(".tab-panel")) {
		panel.classList.toggle("active", panel.id === tabName + "Tab");
	}
	for (const button of document.querySelectorAll(".tab-button")) {
		button.classList.toggle("active", button.dataset.tab === tabName);
	}
}

function installROIControls() {
	const controls = [
		{ kind: "party", action: "openPartyPicker()" },
		{ kind: "death", action: "openDeathPicker()" },
		{ kind: "status", action: "openStatusPicker()" },
		{ kind: "target", action: "openTargetPicker()" }
	];
	for (const control of controls) {
		const setButton = document.querySelector('button[onclick="' + control.action + '"]');
		if (!setButton || setButton.dataset.roiControlInstalled) continue;
		setButton.dataset.roiControlInstalled = "true";
		setButton.innerText = "SET AREA";
		const group = document.createElement("div");
		group.className = "roi-button-group";
		setButton.parentNode.insertBefore(group, setButton);
		group.appendChild(setButton);
		const resetButton = document.createElement("button");
		resetButton.type = "button";
		resetButton.className = "party-picker-button roi-reset-button";
		resetButton.innerText = "RESET";
		resetButton.title = "Reset selected area";
		resetButton.onclick = function() { resetROI(control.kind); };
		group.appendChild(resetButton);
	}
}

async function resetROI(kind) {
	const labels = { party: "Party OCR", death: "death dialog", status: "HP / TP status", target: "target panel" };
	if (!confirm("Reset " + labels[kind] + " area? The related feature will be disabled.")) return;
	try {
		const response = await fetch("/api/" + kind + "-roi/reset", { method: "POST" });
		const result = await response.json();
		if (!result.success) {
			alert(result.message || "Failed to reset selected area.");
			return;
		}
		const preview = document.getElementById(kind + "ROIPreview");
		if (preview) {
			preview.removeAttribute("src");
			preview.style.display = "none";
		}
		const loaders = { party: loadPartyROI, death: loadDeathROI, status: loadStatusROI, target: loadTargetROI };
		await loaders[kind]();
		scheduleLiveConfig();
	} catch (error) {
		console.error("Failed to reset ROI", error);
		alert("Failed to reset selected area.");
	}
}

function buildEmergencySkills() {
	const container = document.getElementById("emergencySkills");
	const options = emergencySkillOptions.map(function(entry) {
		return '<option value="' + entry[0] + '">' + entry[1] + '</option>';
	}).join("");
	container.innerHTML = Array.from({ length: 5 }, function(_, index) {
		const slot = index + 1;
	return '<div class="emergency-card">' +
			'<input class="emergency-check" type="checkbox" id="emergency' + slot + '">' +
			'<select class="emergency-slot" id="emergencySlot' + slot + '" disabled>' + options + '</select>' +
			'<label class="emergency-target-label" for="emergencyNeedTarget' + slot + '">' +
				'<input class="emergency-needs-target" type="checkbox" id="emergencyNeedTarget' + slot + '" disabled>Need Target' +
			'</label>' +
			'</div>';
	}).join("");
	syncEmergencySkillSlots();
}

const supportSkillSlots = [
	["1", 0x31], ["2", 0x32], ["3", 0x33], ["4", 0x34], ["5", 0x35],
	["6", 0x36], ["7", 0x37], ["8", 0x38], ["9", 0x39], ["0", 0x30]
];

function buildSupportSkills() {
	const container = document.getElementById("supportSkills");
	container.innerHTML = supportSkillSlots.map(function(entry) {
		const name = entry[0];
		return '<div class="support-skill-card">' +
			'<input class="support-skill-check" type="checkbox" id="supportSkill' + name + '">' +
			'<label class="skill-name">' + name + '</label>' +
			'<input class="support-skill-delay numeric-input" type="text" inputmode="decimal" id="supportDelay' + name + '" value="0">' +
			'<label class="support-with-target" for="supportWithTarget' + name + '">' +
			'<input class="support-with-target-check" type="checkbox" id="supportWithTarget' + name + '">With Target</label>' +
			'</div>';
	}).join("");
	syncSupportSkillSlots();
}

function getTargetUntilRole() {
	const selected = document.querySelector('input[name="targetUntilRole"]:checked');
	return selected ? selected.value : "attacker";
}

function setTargetUntilRole(role) {
	const radio = document.querySelector('input[name="targetUntilRole"][value="' + role + '"]');
	if (radio) radio.checked = true;
}

function isSupportTargetUntil() {
	return getTargetMode() === "until" && getTargetUntilRole() === "support";
}

function syncSupportSkillSlots() {
	const enabled = hasSelectedTargetWindow() && isSupportTargetUntil();
	for (const entry of supportSkillSlots) {
		const name = entry[0];
		const checkbox = document.getElementById("supportSkill" + name);
		const delay = document.getElementById("supportDelay" + name);
		const withTarget = document.getElementById("supportWithTarget" + name);
		if (!checkbox || !delay || !withTarget) continue;
		checkbox.disabled = !enabled;
		delay.disabled = !enabled || !checkbox.checked;
		withTarget.disabled = !enabled || !checkbox.checked;
	}
}

function syncEmergencySkillSlots() {
	const enabled = hasSelectedTargetWindow() && document.getElementById("autoPotHP").checked;
	const targetEnabled = enabled && targetROIReady;
	const panic = document.getElementById("assistPanicTarget");
	if (panic) {
		// A saved config can contain this option while the target ROI is not
		// available yet. Keep an already enabled option clickable so the user can
		// turn it off; only lock options that would turn it on.
		panic.disabled = !targetEnabled && !panic.checked;
	}
	for (let slot = 1; slot <= 5; slot++) {
		const checkbox = document.getElementById("emergency" + slot);
		const select = document.getElementById("emergencySlot" + slot);
		const needsTarget = document.getElementById("emergencyNeedTarget" + slot);
		if (!checkbox || !select || !needsTarget) continue;
		checkbox.disabled = !enabled;
		select.disabled = !enabled || !checkbox.checked;
		// Same rule for loaded Need Target slots: they must remain uncheckable
		// even before the Target panel has been selected again.
		needsTarget.disabled = !targetEnabled && !needsTarget.checked;
	}
	const hint = document.getElementById("emergencyTargetHint");
	if (hint) {
		hint.innerText = targetROIReady
			? "Need Target skills use the selected Target panel. In normal assist mode KaTools never sends E."
			: "Set Target panel area first to enable Need Target or Assist Panic Target.";
	}
}

function getConfig() {

	const skills = [

		{
			name: "1",
			vk: 0x31,
			enabled: document.getElementById("skill1").checked,
			delay: Number(document.getElementById("delay1").value)
		},

		{
			name: "2",
			vk: 0x32,
			enabled: document.getElementById("skill2").checked,
			delay: Number(document.getElementById("delay2").value)
		},

		{
			name: "3",
			vk: 0x33,
			enabled: document.getElementById("skill3").checked,
			delay: Number(document.getElementById("delay3").value)
		},

		{
			name: "4",
			vk: 0x34,
			enabled: document.getElementById("skill4").checked,
			delay: Number(document.getElementById("delay4").value)
		},

		{
			name: "5",
			vk: 0x35,
			enabled: document.getElementById("skill5").checked,
			delay: Number(document.getElementById("delay5").value)
		},

		{
			name: "6",
			vk: 0x36,
			enabled: document.getElementById("skill6").checked,
			delay: Number(document.getElementById("delay6").value)
		},

		{
			name: "7",
			vk: 0x37,
			enabled: document.getElementById("skill7").checked,
			delay: Number(document.getElementById("delay7").value)
		},

		{
			name: "8",
			vk: 0x38,
			enabled: document.getElementById("skill8").checked,
			delay: Number(document.getElementById("delay8").value)
		},

		{
			name: "9",
			vk: 0x39,
			enabled: document.getElementById("skill9").checked,
			delay: Number(document.getElementById("delay9").value)
		},

		{
			name: "0",
			vk: 0x30,
			enabled: document.getElementById("skill0").checked,
			delay: Number(document.getElementById("delay0").value)
		},

		{
			name: "F1",
			vk: 0x70,
			enabled: document.getElementById("skillF1").checked,
			delay: Number(document.getElementById("delayF1").value)
		},

		{
			name: "F2",
			vk: 0x71,
			enabled: document.getElementById("skillF2").checked,
			delay: Number(document.getElementById("delayF2").value)
		},

		{
			name: "F3",
			vk: 0x72,
			enabled: document.getElementById("skillF3").checked,
			delay: Number(document.getElementById("delayF3").value)
		},

		{
			name: "F4",
			vk: 0x73,
			enabled: document.getElementById("skillF4").checked,
			delay: Number(document.getElementById("delayF4").value)
		},

		{
			name: "F5",
			vk: 0x74,
			enabled: document.getElementById("skillF5").checked,
			delay: Number(document.getElementById("delayF5").value)
		},

		{
			name: "F6",
			vk: 0x75,
			enabled: document.getElementById("skillF6").checked,
			delay: Number(document.getElementById("delayF6").value)
		},

		{
			name: "F7",
			vk: 0x76,
			enabled: document.getElementById("skillF7").checked,
			delay: Number(document.getElementById("delayF7").value)
		},

		{
			name: "F8",
			vk: 0x77,
			enabled: document.getElementById("skillF8").checked,
			delay: Number(document.getElementById("delayF8").value)
		},

		{
			name: "F9",
			vk: 0x78,
			enabled: document.getElementById("skillF9").checked,
			delay: Number(document.getElementById("delayF9").value)
		},

		{
			name: "F10",
			vk: 0x79,
			enabled: document.getElementById("skillF10").checked,
			delay: Number(document.getElementById("delayF10").value)
		}

	];

	const supportSkills = supportSkillSlots.map(function(entry) {
		const name = entry[0];
		return {
			name: name,
			vk: entry[1],
			enabled: document.getElementById("supportSkill" + name).checked,
			delay: Number(document.getElementById("supportDelay" + name).value),
			withTarget: document.getElementById("supportWithTarget" + name).checked
		};
	});

	return {

		hwnd:
			document.getElementById("windowSelect").value,

		autoAcceptEnabled:
			document.getElementById("autoAccept").checked,

		autoPauseDeathEnabled:
			document.getElementById("autoPauseDeath").checked,

		autoResurrectEnabled:
			document.getElementById("autoResurrect").checked,

		assistSkillVK:
			Number(document.getElementById("assistSkillSlot").value),

		assistSkillDelay:
			Number(document.getElementById("assistSkillDelay").value),

		targetEnabled:
			getTargetMode() === "normal",

		targetUntilDeadEnabled:
			getTargetMode() === "until",

		targetUntilDeadSupport:
			isSupportTargetUntil(),

		targetUntilDeadCharacterName:
			document.getElementById("targetUntilDeadCharacterName").value,

		targetNameFilterMode:
			getTargetNameFilterMode(),

		targetDelay:
			Number(document.getElementById("targetDelay").value),

		attackEnabled:
			document.getElementById("attack").checked,

		attackDelay:
			Number(document.getElementById("attackDelay").value),

		pickEnabled:
			document.getElementById("pick").checked,

		pickDelay:
			Number(document.getElementById("pickDelay").value),

		autoPotHPEnabled: document.getElementById("autoPotHP").checked,
		autoPotHPPercent: Number(document.getElementById("autoPotHPPercent").value),
		autoPotHPSlotVK: Number(document.getElementById("autoPotHPSlot").value),
		autoPotTPEnabled: document.getElementById("autoPotTP").checked,
		autoPotTPPercent: Number(document.getElementById("autoPotTPPercent").value),
		autoPotTPSlotVK: Number(document.getElementById("autoPotTPSlot").value),

		emergencySkills: Array.from({ length: 5 }, function(_, index) {
			const slot = index + 1;
			return {
				enabled: document.getElementById("autoPotHP").checked && document.getElementById("emergency" + slot).checked,
				vk: Number(document.getElementById("emergencySlot" + slot).value),
				needsTarget: document.getElementById("emergencyNeedTarget" + slot).checked
			};
		}),

		assistPanicTarget: document.getElementById("assistPanicTarget").checked,

		skills: skills,

		supportSkills: supportSkills
	};
}

function numericConfigError(config) {
	function positive(value, label) {
		if (!Number.isFinite(value) || value <= 0) {
			return label + " must be a number greater than zero.";
		}
		return "";
	}
	function percent(value, label) {
		if (!Number.isFinite(value) || value < 1 || value > 100) {
			return label + " must be a number from 1 to 100.";
		}
		return "";
	}

	let error = "";
	if (config.autoPotHPEnabled && (error = percent(config.autoPotHPPercent, "HP Pot threshold"))) return error;
	if (config.autoPotTPEnabled && (error = percent(config.autoPotTPPercent, "TP Pot threshold"))) return error;
	if (config.assistSkillVK !== 0 && (error = positive(config.assistSkillDelay, "Assist skill interval"))) return error;
	if (config.targetEnabled && (error = positive(config.targetDelay, "Target interval"))) return error;
	const supportMode = config.targetUntilDeadEnabled && config.targetUntilDeadSupport;
	if (config.attackEnabled && !supportMode && (error = positive(config.attackDelay, "Attack interval"))) return error;
	if (config.pickEnabled && (error = positive(config.pickDelay, "Pick interval"))) return error;
	const activeSkills = supportMode ? config.supportSkills : config.skills;
	for (const skill of activeSkills) {
		const label = supportMode ? "Support Skill " : "Skill ";
		if (skill.enabled && (error = positive(skill.delay, label + skill.name + " interval"))) return error;
	}
	return "";
}

function validNumericConfig(config, showAlert) {
	const error = numericConfigError(config);
	if (!error) return true;
	if (showAlert) alert(error);
	return false;
}


async function toggleBot() {

	const response =
		await fetch("/api/status");

	const result =
		await response.json();

	if (result.running) {

		await stopBot();

	} else {

		await startBot();

	}

}


async function startBot() {

	const config = getConfig();
	if (!validNumericConfig(config, true)) {
		return;
	}

	if (!config.hwnd) {

		alert(
			"Please select a target window first."
		);

		return;
	}

	const response =
		await fetch(
			"/api/start",
			{
				method: "POST",

				headers: {
					"Content-Type":
						"application/json"
				},

				body:
					JSON.stringify(config)
			}
		);

	const result =
		await response.json();

	if (!result.success) {

		alert(
			result.message ||
			"Failed to start KaTools."
		);

		return;
	}

	botRuntimeActive = true;
	updateStatus();
}


async function stopBot() {

	const response =
		await fetch(
			"/api/stop",
			{
				method: "POST"
			}
		);

	const result =
		await response.json();

	if (!result.success) {

		alert(
			result.message ||
			"Failed to stop bot."
		);

		return;
	}

	botRuntimeActive = false;
	updateStatus();
}


// ============================================================
// LIVE CONFIG
// ============================================================

let liveConfigTimer = null;
let savedWindowHWND = "";

function scheduleLiveConfig() {

	if (liveConfigTimer) {
		clearTimeout(liveConfigTimer);
	}

	liveConfigTimer = setTimeout(applyLiveConfig, 300);
}

async function applyLiveConfig() {

	liveConfigTimer = null;
	const config = getConfig();
	if (!validNumericConfig(config, false)) {
		return;
	}

	try {
		const response = await fetch(
			"/api/config",
			{
				method: "POST",
				headers: {
					"Content-Type": "application/json"
				},
				body: JSON.stringify(config)
			}
		);

		const result = await response.json();
		if (!result.success) {
			console.error("Live config update failed:", result.message);
		}
	} catch (error) {
		console.error("Live config update failed:", error);
	}
}

function applySavedConfig(config) {
	if (!config) {
		return;
	}

	savedWindowHWND = config.hwnd || "";
	document.getElementById("autoAccept").checked = !!config.autoAcceptEnabled;
	// Saved config is applied before the selected window and Death ROI finish
	// loading. Do not discard these values merely because the controls are
	// temporarily disabled; the ROI availability pass below will clear them
	// only when the required area is genuinely missing.
	document.getElementById("autoPauseDeath").checked = !!config.autoPauseDeathEnabled;
	document.getElementById("autoResurrect").checked =
		!!config.autoPauseDeathEnabled && !!config.autoResurrectEnabled;
	setTargetMode(config.targetUntilDeadEnabled ? "until" : (config.targetEnabled ? "normal" : "assist"));
	setTargetUntilRole(config.targetUntilDeadSupport ? "support" : "attacker");
	document.getElementById("assistSkillSlot").value = config.assistSkillVK || "0";
	document.getElementById("assistSkillDelay").value = config.assistSkillDelay || "1";
	document.getElementById("targetDelay").value = config.targetDelay || "";
	document.getElementById("targetUntilDeadCharacterName").value =
		config.targetUntilDeadCharacterName || "";
	// targetWithoutName was used by a short-lived separate Target mode. Keep
	// those saved presets compatible by showing the equivalent filter choice.
	setTargetNameFilterMode(config.targetWithoutName ? "none" : (config.targetNameFilterMode || "skip"));
	syncTargetMode();
	document.getElementById("attack").checked = !!config.attackEnabled;
	document.getElementById("attackDelay").value = config.attackDelay || "";
	document.getElementById("pick").checked = !!config.pickEnabled;
	document.getElementById("pickDelay").value = config.pickDelay || "";
	document.getElementById("autoPotHP").checked = !!config.autoPotHPEnabled;
	document.getElementById("autoPotHPPercent").value = config.autoPotHPPercent || "";
	document.getElementById("autoPotHPSlot").value = config.autoPotHPSlotVK || "49";
	document.getElementById("autoPotTP").checked = !!config.autoPotTPEnabled;
	document.getElementById("autoPotTPPercent").value = config.autoPotTPPercent || "";
	document.getElementById("autoPotTPSlot").value = config.autoPotTPSlotVK || "49";
	for (let index = 0; index < 5; index++) {
		const slot = (config.emergencySkills || [])[index] || {};
		document.getElementById("emergency" + (index + 1)).checked = !!slot.enabled;
		document.getElementById("emergencySlot" + (index + 1)).value = slot.vk || "49";
		document.getElementById("emergencyNeedTarget" + (index + 1)).checked = !!slot.needsTarget;
	}
	document.getElementById("assistPanicTarget").checked = !!config.assistPanicTarget;
	syncEmergencySkillSlots();

	for (const skill of config.skills || []) {
		const checkbox = document.getElementById("skill" + skill.name);
		const delay = document.getElementById("delay" + skill.name);
		if (checkbox) checkbox.checked = !!skill.enabled;
		if (delay) delay.value = skill.delay || "";
	}
	for (const skill of config.supportSkills || []) {
		const checkbox = document.getElementById("supportSkill" + skill.name);
		const delay = document.getElementById("supportDelay" + skill.name);
		const withTarget = document.getElementById("supportWithTarget" + skill.name);
		if (checkbox) checkbox.checked = !!skill.enabled;
		if (delay) delay.value = skill.delay || "";
		if (withTarget) withTarget.checked = !!skill.withTarget;
	}
	syncTargetMode();
}

let configProfileDialogMode = "";
let configProfileNames = [];

async function getConfigProfileNames() {
	const response = await fetch("/api/config/profiles");
	const result = await response.json();
	if (!result.success) {
		throw new Error(result.message || "Failed to load config profiles.");
	}
	return {
		profiles: result.profiles || [],
		legacyFound: !!result.legacyFound
	};
}

function closeConfigProfileDialog() {
	const modal = document.getElementById("configProfileModal");
	modal.hidden = true;
	configProfileDialogMode = "";
}

function openSaveConfigDialog() {
	configProfileDialogMode = "save";
	const modal = document.getElementById("configProfileModal");
	const input = document.getElementById("configProfileName");
	const list = document.getElementById("configProfileList");
	document.getElementById("configProfileTitle").innerText = "Save Character Config";
	document.getElementById("configProfileHint").innerText = "Enter the character name for this config.";
	document.getElementById("configProfileConfirm").innerText = "SAVE";
	list.hidden = true;
	input.hidden = false;
	input.value = "";
	modal.hidden = false;
	input.focus();
}

async function openLoadConfigDialog() {
	try {
		const configProfiles = await getConfigProfileNames();
		configProfileNames = configProfiles.profiles;
		if (configProfiles.legacyFound) {
			configProfileNames.push("__legacy__");
		}
	} catch (error) {
		console.error("Failed to list config profiles", error);
		alert(error.message || "Failed to load config profiles.");
		return;
	}
	if (configProfileNames.length === 0) {
		alert("No character configs have been saved yet.");
		return;
	}

	configProfileDialogMode = "load";
	const modal = document.getElementById("configProfileModal");
	const input = document.getElementById("configProfileName");
	const list = document.getElementById("configProfileList");
	document.getElementById("configProfileTitle").innerText = "Load Character Config";
	document.getElementById("configProfileHint").innerText = "Choose a saved character config.";
	document.getElementById("configProfileConfirm").innerText = "LOAD";
	input.hidden = true;
	list.hidden = false;
	list.replaceChildren();
	for (const name of configProfileNames) {
		const option = document.createElement("option");
		option.value = name;
		option.textContent = name === "__legacy__" ? "Default (katools_config.json)" : name;
		list.appendChild(option);
	}
	modal.hidden = false;
	list.focus();
}

async function confirmConfigProfileDialog() {
	if (configProfileDialogMode === "save") {
		const name = document.getElementById("configProfileName").value.trim();
		if (!name) {
			alert("Enter a character name.");
			return;
		}
		try {
			const configProfiles = await getConfigProfileNames();
			configProfileNames = configProfiles.profiles;
		} catch (error) {
			console.error("Failed to list config profiles", error);
			alert(error.message || "Failed to save config.");
			return;
		}
		if (configProfileNames.some(function(profileName) { return profileName.toLowerCase() === name.toLowerCase(); }) &&
			!confirm('Replace the saved config for "' + name + '"?')) {
			return;
		}
		await saveCurrentConfig(name);
		return;
	}

	if (configProfileDialogMode === "load") {
		const name = document.getElementById("configProfileList").value;
		if (name) {
			await loadSavedConfig(name);
		}
	}
}

// The profile dialog is not a form, so browsers do not provide an implicit
// submit action for its text input. Let Enter perform the same save/load as
// the visible confirmation button, while keeping Escape as the quick cancel.
document.getElementById("configProfileModal").addEventListener("keydown", function(event) {
	if (event.isComposing) {
		return;
	}
	if (event.key === "Escape") {
		event.preventDefault();
		closeConfigProfileDialog();
		return;
	}
	if (event.key === "Enter") {
		event.preventDefault();
		confirmConfigProfileDialog();
	}
});

async function saveCurrentConfig(profileName) {
	const config = getConfig();
	if (!validNumericConfig(config, true)) {
		return;
	}
	try {
		const response = await fetch("/api/config/profile", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ name: profileName, config: config })
		});
		const result = await response.json();
		if (!result.success) {
			alert(result.message || "Failed to save config.");
			return;
		}
		closeConfigProfileDialog();
		alert('Config for "' + result.name + '" saved.');
	} catch (error) {
		console.error("Failed to save config", error);
		alert("Failed to save config.");
	}
}

async function loadSavedConfig(profileName) {
	try {
		const legacyConfig = profileName === "__legacy__";
		const response = await fetch(legacyConfig ? "/api/config" : "/api/config/profile?name=" + encodeURIComponent(profileName));
		const result = await response.json();
		if (result.success && result.found) {
			applySavedConfig(result.config);
			await loadWindows();
			closeConfigProfileDialog();
			alert(legacyConfig ? "Default config loaded." : 'Config for "' + result.name + '" loaded.');
		} else {
			alert("Saved character config was not found.");
		}
	} catch (error) {
		console.error("Failed to load saved config", error);
		alert("Failed to load character config.");
	}
}

async function updateStatus() {

	try {

		const response =
			await fetch("/api/status");

		const result =
			await response.json();

		const status =
			document.getElementById("status");

		const button =
			document.getElementById("botButton");

		const info =
			document.getElementById("windowInfo");

		botRuntimeActive = !!(result.running || result.starting);

		if (result.running || result.starting) {

			if (result.paused) {
				status.className = "status paused";
				status.innerText = "● AUTO PAUSED — DEATH DETECTED";
			} else {
				status.className = "status running";
				status.innerText = result.starting ? "● STARTING..." : "● BOT RUNNING";
			}

			button.className =
				"bot-button stop";

			button.innerText =
				"STOP BOT";

			if (result.title) {

				info.innerText =
					result.title +
					"  |  " +
					result.hwnd;
			}

		} else {

			status.className =
				"status stopped";

			status.innerText =
				"● BOT STOPPED";

			button.className =
				"bot-button start";

			button.innerText =
				"START BOT";

			if (result.title) {

				info.innerText =
					"Selected: " +
					result.title +
					"  |  " +
					result.hwnd;

			} else {

				info.innerText =
					"Select the game window before pressing START.";
			}
		}

		const statusRead = document.getElementById("statusReadInfo");
		const pot = result.autoPot;
		if (pot && pot.lastRead && (pot.hpMax > 0 || pot.tpMax > 0)) {
			const hpText = pot.hpMax > 0
				? "HP " + pot.hpCurrent + "/" + pot.hpMax + " (" + (pot.hpCurrent * 100 / pot.hpMax).toFixed(1) + "%)"
				: "HP scanning...";
			const tpText = pot.tpMax > 0
				? "TP " + pot.tpCurrent + "/" + pot.tpMax + " (" + (pot.tpCurrent * 100 / pot.tpMax).toFixed(1) + "%)"
				: "TP scanning...";
			statusRead.innerText = "Last scan: " + hpText + "  |  " + tpText;
		} else {
			statusRead.innerText = "";
		}

	} catch (error) {

		console.error(error);

	}

}


function hasSelectedTargetWindow() {
	return !!document.getElementById("windowSelect").value;
}

function updateAppTitle() {
	const select = document.getElementById("windowSelect");
	const selected = select.options[select.selectedIndex];
	const title = selected && select.value ? selected.textContent : "";
	const appTitle = title ? "KaTools - " + title : "KaTools";
	document.title = appTitle;
	document.getElementById("appTitle").innerText = appTitle;
}

function updateWindowDependentControls() {
	const ready = hasSelectedTargetWindow();

	for (const button of document.querySelectorAll("button:not(.refresh-button)")) {
		button.disabled = !ready;
	}

	for (const selector of [
		"input[name=targetMode]", "#assistSkillSlot", "#assistSkillDelay", "#targetDelay", "#attack", "#attackDelay", "#pick", "#pickDelay",
		"#targetUntilDeadCharacterName", "input[name=targetUntilRole]", "input[name=targetNameFilterMode]", ".skill-check", ".skill-delay", ".support-skill-check", ".support-skill-delay", ".support-with-target-check", ".emergency-check", ".emergency-slot", ".emergency-needs-target", "#assistPanicTarget"
	]) {
		for (const control of document.querySelectorAll(selector)) {
			control.disabled = !ready;
		}
	}

	if (!ready) {
		for (const id of ["autoAccept", "autoPauseDeath", "autoResurrect", "autoPotHP", "autoPotTP"]) {
			document.getElementById(id).disabled = true;
		}
		for (const id of ["autoPotHPPercent", "autoPotHPSlot", "autoPotTPPercent", "autoPotTPSlot"]) {
			document.getElementById(id).disabled = true;
		}
		return;
	}

	// ROI-dependent controls need both a selected window and their own ROI.
	loadPartyROI();
	loadDeathROI();
	loadTargetROI();
	loadStatusROI();
	syncTargetMode();
	syncEmergencySkillSlots();
}

async function loadWindows() {
	if (windowsLoading) {
		return;
	}
	const refreshButton = document.getElementById("refreshWindowsButton");
	windowsLoading = true;
	refreshButton.disabled = true;
	refreshButton.classList.add("loading");
	refreshButton.setAttribute("aria-busy", "true");
	refreshButton.textContent = "REFRESHING...";

	try {

		const response =
			await fetch("/api/windows");

		const result =
			await response.json();

		const select =
			document.getElementById(
				"windowSelect"
			);

		const current =
			select.value || savedWindowHWND;

		select.innerHTML = "";

		const placeholder = document.createElement("option");
		placeholder.value = "";
		placeholder.textContent = "Select a game window...";
		select.appendChild(placeholder);

		if (
			!result.windows ||
			result.windows.length === 0
		) {

			placeholder.textContent = "No visible windows found";
			updateAppTitle();
			updateWindowDependentControls();

			return;
		}

		for (
			const item of result.windows
		) {

			const option =
				document.createElement(
					"option"
				);

			option.value =
				item.hwnd;

			option.textContent = item.title;

			select.appendChild(option);
		}

		if (current && Array.from(select.options).some(option => option.value === current)) {
			select.value = current;
			savedWindowHWND = select.value;
		} else {
			select.value = "";
			savedWindowHWND = "";
		}

		select.onchange = function() {

			const selected =
				select.options[
					select.selectedIndex
				];

			if (selected) {

				document.getElementById(
					"windowInfo"
				).innerText =
					"Selected: " +
					selected.textContent;
			}

			savedWindowHWND = select.value;
			updateAppTitle();
			updateWindowDependentControls();
		};

		updateAppTitle();
		updateWindowDependentControls();

	} catch (error) {

		console.error(error);

		alert(
			"Failed to load windows."
		);

	} finally {
		windowsLoading = false;
		refreshButton.disabled = false;
		refreshButton.classList.remove("loading");
		refreshButton.removeAttribute("aria-busy");
		refreshButton.textContent = "REFRESH";
	}
}


// ============================================================
// PARTY ROI
// ============================================================

async function loadPartyROI() {

	try {

		const response =
			await fetch("/api/party-roi");

		const result =
			await response.json();

		if (!result.success) {
			return;
		}

		document.getElementById(
			"partyROIInfo"
		).innerText =
			"Party OCR ROI: X=" +
			result.x +
			" Y=" +
			result.y +
			" W=" +
			result.w +
			" H=" +
			result.h;

		updateAutoAcceptAvailability(!!result.selected);

	} catch (error) {

		console.error(
			"Failed to load party ROI:",
			error
		);

	}

}

function updateAutoAcceptAvailability(selected) {
	const checkbox = document.getElementById("autoAccept");
	const label = document.querySelector('label[for="autoAccept"]');

	checkbox.disabled = !selected || !hasSelectedTargetWindow();
	if (!selected) {
		checkbox.checked = false;
		label.title = "Select Party OCR area first.";
	} else {
		label.title = "";
	}
}

async function loadDeathROI() {
	try {
		const response = await fetch("/api/death-roi");
		const result = await response.json();
		const info = document.getElementById("deathROIInfo");
		if (!result.success || !result.selected) {
			info.innerText = "Death dialog ROI: select the full Message dialog first";
			updateAutoPauseDeathAvailability(false);
			return;
		}
		info.innerText = "Death dialog ROI: X=" + result.x + " Y=" + result.y +
			" W=" + result.w + " H=" + result.h;
		updateAutoPauseDeathAvailability(true);
	} catch (error) {
		console.error("Failed to load death ROI:", error);
	}
}

function updateAutoPauseDeathAvailability(selected) {
	const checkbox = document.getElementById("autoPauseDeath");
	const label = document.querySelector('label[for="autoPauseDeath"]');
	const autoResurrect = document.getElementById("autoResurrect");
	const resuLabel = document.querySelector('label[for="autoResurrect"]');
	checkbox.disabled = !selected || !hasSelectedTargetWindow();
	autoResurrect.disabled = !selected || !hasSelectedTargetWindow() || !checkbox.checked;
	if (!selected) {
		checkbox.checked = false;
		autoResurrect.checked = false;
		label.title = "Select the full death dialog area first.";
		resuLabel.title = "Select the full death dialog area and enable Auto Pause on Death first.";
	} else {
		label.title = "";
		resuLabel.title = autoResurrect.disabled ? "Enable Auto Pause on Death first." : "";
	}
}

async function loadTargetROI() {
	try {
		const response = await fetch("/api/target-roi");
		const result = await response.json();
		const info = document.getElementById("targetROIInfo");
		if (!result.success || !result.selected) {
			targetROIReady = false;
			info.innerText = "Target panel area: select the name and red HP bar first";
			updateTargetUntilAvailability(false);
			syncEmergencySkillSlots();
			return;
		}
		targetROIReady = true;
		info.innerText = "Target panel area: X=" + result.x + " Y=" + result.y +
			" W=" + result.w + " H=" + result.h;
		updateTargetUntilAvailability(true);
		syncEmergencySkillSlots();
	} catch (error) {
		console.error("Failed to load target monster ROI:", error);
	}
}

function updateTargetUntilAvailability(selected) {
	const untilRadio = document.getElementById("targetUntilDead");
	// Selecting this mode must be possible before a Target panel ROI exists:
	// its Set Area control is how the ROI gets created. Start still validates
	// the required window and ROI server-side.
	untilRadio.disabled = false;
	untilRadio.parentElement.title = "";
	syncTargetMode();
}

function getTargetMode() {
	const selected = document.querySelector('input[name="targetMode"]:checked');
	return selected ? selected.value : "assist";
}

function setTargetMode(mode) {
	const radio = document.querySelector('input[name="targetMode"][value="' + mode + '"]');
	if (radio && !radio.disabled) radio.checked = true;
}

function getTargetNameFilterMode() {
	const selected = document.querySelector('input[name="targetNameFilterMode"]:checked');
	return selected ? selected.value : "skip";
}

function setTargetNameFilterMode(mode) {
	const value = mode === "whitelist" || mode === "none" ? mode : "skip";
	const radio = document.querySelector('input[name="targetNameFilterMode"][value="' + value + '"]');
	if (radio) radio.checked = true;
}

function syncTargetNameFilterMode() {
	const filterMode = getTargetNameFilterMode();
	const withoutName = filterMode === "none";
	const whitelist = filterMode === "whitelist";
	document.getElementById("targetNameListLabel").textContent = whitelist ? "Whitelist Target Names" : "Skip Target Names";
	document.getElementById("targetUntilDeadCharacterName").placeholder = whitelist ? "Example: Vasabhum;Zarku Rudhira" : "Example: MangAep;Dadati";
	document.getElementById("targetNameListHint").textContent = whitelist
		? "Only names on this list may be attacked. Required for Target and Target Until Dead. Separate names with a semicolon."
		: "Optional for Target. Required for Target Until Dead. Separate names with a semicolon.";
	document.getElementById("targetNameListLabel").hidden = withoutName;
	document.getElementById("targetUntilDeadCharacterName").hidden = withoutName;
	document.getElementById("targetNameListHint").hidden = withoutName;
}

function syncTargetMode() {
	const windowReady = hasSelectedTargetWindow();
	const untilRadio = document.getElementById("targetUntilDead");
	untilRadio.disabled = false;
	const mode = getTargetMode();
	const supportMode = mode === "until" && getTargetUntilRole() === "support";
	const usesSkipNames = mode === "normal" || mode === "until";
	// Target Until Dead always identifies names. If a user switches from
	// Target > Without name, restore a real filter before enabling it.
	if (mode === "until" && getTargetNameFilterMode() === "none") {
		setTargetNameFilterMode("skip");
	}
	const usesNameList = mode === "until" || (mode === "normal" && getTargetNameFilterMode() !== "none");
	document.getElementById("assistSkillSlot").disabled = !windowReady || mode !== "assist";
	document.getElementById("assistSkillDelay").disabled = !windowReady || mode !== "assist";
	const delay = document.getElementById("targetDelay");
	delay.disabled = !windowReady || mode !== "normal";
	document.getElementById("targetAssistSettings").hidden = mode !== "assist";
	document.getElementById("targetNormalSettings").hidden = mode !== "normal";
	document.getElementById("targetSkipNamesSettings").hidden = !usesSkipNames;
	document.getElementById("targetUntilDeadCharacterName").disabled = !windowReady || !usesNameList;
	document.getElementById("targetUntilSettings").hidden = mode !== "until";
	document.getElementById("targetSupportSkills").hidden = !supportMode;
	document.getElementById("normalNumberSkillsSection").hidden = supportMode;
	document.getElementById("normalFunctionSkillsSection").hidden = supportMode;
	for (const role of document.querySelectorAll('input[name="targetUntilRole"]')) {
		role.disabled = !windowReady || mode !== "until";
	}
	for (const filterMode of document.querySelectorAll('input[name="targetNameFilterMode"]')) {
		filterMode.disabled = !windowReady || !usesSkipNames || (mode === "until" && filterMode.value === "none");
	}
	document.getElementById("targetWithoutNameOption").hidden = mode !== "normal";
	syncTargetNameFilterMode();
	// Keep the saved Attack checkbox intact for Attacker mode, but do not allow
	// R to compete with Support Skills while Support is selected.
	document.getElementById("attack").disabled = !windowReady || supportMode;
	document.getElementById("attackDelay").disabled = !windowReady || supportMode;
	syncSupportSkillSlots();
}

async function loadTargetPreview() {
	const preview = document.getElementById("targetROIPreview");
	try {
		const response = await fetch("/api/target-roi/preview", { cache: "no-store" });
		if (!response.ok) {
			preview.style.display = "none";
			return;
		}
		preview.src = "/api/target-roi/preview?t=" + Date.now();
		preview.style.display = "block";
	} catch (error) {
		preview.style.display = "none";
	}
}

async function openTargetPicker() {
	const hwnd = document.getElementById("windowSelect").value;
	if (!hwnd) {
		alert("Please select a target window first.");
		return;
	}
	const response = await fetch("/api/target-roi/picker", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ hwnd: hwnd })
	});
	const result = await response.json();
	if (!result.success) {
		alert(result.message || "Failed to open Target panel picker.");
		return;
	}
	let refreshes = 0;
	const timer = setInterval(async function() {
		await loadTargetROI();
		await loadTargetPreview();
		if (++refreshes >= 30) clearInterval(timer);
	}, 1000);
}

async function loadDeathPreview() {
	const preview = document.getElementById("deathROIPreview");
	try {
		const response = await fetch("/api/death-roi/preview", { cache: "no-store" });
		if (!response.ok) {
			preview.style.display = "none";
			return;
		}
		preview.src = "/api/death-roi/preview?t=" + Date.now();
		preview.style.display = "block";
	} catch (error) {
		preview.style.display = "none";
	}
}

async function openDeathPicker() {
	const hwnd = document.getElementById("windowSelect").value;
	if (!hwnd) {
		alert("Please select a target window first.");
		return;
	}
	const response = await fetch("/api/death-roi/picker", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ hwnd: hwnd })
	});
	const result = await response.json();
	if (!result.success) {
		alert(result.message || "Failed to open Death dialog ROI picker.");
		return;
	}
	let refreshes = 0;
	const timer = setInterval(async function() {
		await loadDeathROI();
		await loadDeathPreview();
		if (++refreshes >= 30) clearInterval(timer);
	}, 1000);
}

async function loadPartyPreview() {

	const preview = document.getElementById("partyROIPreview");

	try {

		const response = await fetch("/api/party-roi/preview", {
			cache: "no-store"
		});

		if (!response.ok) {
			preview.style.display = "none";
			return;
		}

		preview.src = "/api/party-roi/preview?t=" + Date.now();
		preview.style.display = "block";

	} catch (error) {

		preview.style.display = "none";
	}
}

async function loadStatusROI() {
	try {
		const response = await fetch("/api/status-roi");
		const result = await response.json();
		const info = document.getElementById("statusROIInfo");
		if (!result.success || !result.selected) {
			info.innerText = "Select HP and TP bars first";
			updateAutoPotAvailability(false);
			return;
		}
		info.innerText = "Status area: X=" + result.x + " Y=" + result.y + " W=" + result.w + " H=" + result.h;
		updateAutoPotAvailability(true);
	} catch (error) {
		console.error("Failed to load status ROI", error);
	}
}

async function loadStatusPreview() {
	const preview = document.getElementById("statusROIPreview");

	try {
		const response = await fetch("/api/status-roi/preview", {
			cache: "no-store"
		});
		if (!response.ok) {
			preview.style.display = "none";
			return;
		}

		preview.src = "/api/status-roi/preview?t=" + Date.now();
		preview.style.display = "block";
	} catch (error) {
		preview.style.display = "none";
	}
}

function updateAutoPotAvailability(selected) {
	const enabled = selected && hasSelectedTargetWindow();
	for (const id of ["autoPotHP", "autoPotTP"]) {
		const checkbox = document.getElementById(id);
		checkbox.disabled = !enabled;
		if (!selected) checkbox.checked = false;
	}
	for (const id of ["autoPotHPPercent", "autoPotHPSlot", "autoPotTPPercent", "autoPotTPSlot"]) {
		document.getElementById(id).disabled = !enabled;
	}
	syncEmergencySkillSlots();
}

async function openStatusPicker() {
	const hwnd = document.getElementById("windowSelect").value;
	if (!hwnd) {
		alert("Please select a target window first.");
		return;
	}
	const response = await fetch("/api/status-roi/picker", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ hwnd: hwnd })
	});
	const result = await response.json();
	if (!result.success) {
		alert(result.message || "Failed to open Status ROI picker.");
		return;
	}
	let refreshes = 0;
	const timer = setInterval(async function() {
		await loadStatusROI();
		await loadStatusPreview();
		if (++refreshes >= 30) clearInterval(timer);
	}, 1000);
}


async function openPartyPicker() {

	const hwnd =
		document.getElementById(
			"windowSelect"
		).value;

	if (!hwnd) {

		alert(
			"Please select a target window first."
		);

		return;
	}

	const response =
		await fetch(
			"/api/party-roi/picker",
			{
				method: "POST",

				headers: {
					"Content-Type":
						"application/json"
				},

				body: JSON.stringify({
					hwnd: hwnd
				})
			}
		);

	const result =
		await response.json();

	if (!result.success) {

		alert(
			result.message ||
			"Failed to open Party ROI picker."
		);

		return;
	}

	console.log(
		"Party ROI picker opened."
	);

	// Picker berjalan di native overlay, jadi hasilnya tersedia setelah user
	// melepas mouse. Refresh preview selama picker kemungkinan masih terbuka.
	let previewRefreshes = 0;
	const previewTimer = setInterval(async function() {
		await loadPartyROI();
		await loadPartyPreview();
		previewRefreshes++;
		if (previewRefreshes >= 30) {
			clearInterval(previewTimer);
		}
	}, 1000);

}


async function initializeUI() {
	installROIControls();
	buildEmergencySkills();
	buildSupportSkills();
	updateWindowDependentControls();
	await loadWindows();
	loadPartyROI();
	loadPartyPreview();
	loadDeathROI();
	loadDeathPreview();
	loadTargetROI();
	loadTargetPreview();
	loadStatusROI();
	loadStatusPreview();
	updateStatus();
}

initializeUI();

// Closing the browser must not silently leave an active KaTools runtime
// running in the background. Modern browsers intentionally show their own
// fixed wording here, rather than an app-defined message.
window.addEventListener("beforeunload", function(event) {
	if (!botRuntimeActive) {
		return;
	}
	event.preventDefault();
	event.returnValue = "";
});

// pagehide fires only when the page is truly leaving. Keeping the exit beacon
// here means clicking the browser's "Stay" choice above does not stop KaTools.
// Reloading or navigating away is intentionally treated as closing KaTools.
window.addEventListener("pagehide", function() {
	navigator.sendBeacon("/api/exit", "browser-closed");
});

// Do not let an accidental keyboard refresh terminate the local app. Browser
// chrome's toolbar refresh cannot be disabled by a web page, but F5/Ctrl+R
// while the KaTools page is focused can be safely intercepted here.
window.addEventListener("keydown", function(event) {
	const isRefreshKey = event.key === "F5" ||
		((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "r");
	if (!isRefreshKey) {
		return;
	}
	event.preventDefault();
	event.stopPropagation();
}, true);

// Timer and percentage fields accept digits and one decimal separator only.
// Keep them as text inputs so Firefox's number-input quirks cannot introduce
// exponent signs or other characters while the player is editing a value.
document.addEventListener("input", function(event) {
	const input = event.target;
	if (!input.classList || !input.classList.contains("numeric-input")) {
		return;
	}
	const raw = input.value;
	let sanitized = "";
	let hasDecimal = false;
	for (const character of raw) {
		if (character >= "0" && character <= "9") {
			sanitized += character;
		} else if (character === "." && !hasDecimal) {
			sanitized += character;
			hasDecimal = true;
		}
	}
	if (raw !== sanitized) {
		input.value = sanitized;
	}
});

// Apply after a control's value is committed. In particular, number inputs
// must not restart the scheduler while the user is still typing a value.
document.addEventListener("change", function(event) {
	if (event.target.name === "targetMode" || event.target.name === "targetUntilRole" || event.target.name === "targetNameFilterMode") {
		syncTargetMode();
	}
	if (event.target.id === "autoPauseDeath") {
		loadDeathROI();
	}
	if (event.target.id === "autoPotHP" || event.target.classList.contains("emergency-check") || event.target.classList.contains("emergency-needs-target")) {
		syncEmergencySkillSlots();
	}
	if (event.target.classList.contains("support-skill-check")) {
		syncSupportSkillSlots();
	}
	if (event.target.id !== "windowSelect") {
		scheduleLiveConfig();
	}
});

setInterval(
	updateStatus,
	2000
);

</script>

</body>

</html>
`

// ============================================================
// WEB CONFIG
// ============================================================

type WebBotConfig struct {
	HWND string `json:"hwnd"`

	AutoAcceptEnabled            bool    `json:"autoAcceptEnabled"`
	AutoPauseDeathEnabled        bool    `json:"autoPauseDeathEnabled"`
	AutoResurrectEnabled         bool    `json:"autoResurrectEnabled"`
	AssistSkillVK                uintptr `json:"assistSkillVK"`
	AssistSkillDelay             float64 `json:"assistSkillDelay"`
	TargetUntilDeadEnabled       bool    `json:"targetUntilDeadEnabled"`
	TargetUntilDeadSupport       bool    `json:"targetUntilDeadSupport"`
	TargetUntilDeadCharacterName string  `json:"targetUntilDeadCharacterName"`
	TargetNameFilterMode         string  `json:"targetNameFilterMode"`

	TargetEnabled     bool    `json:"targetEnabled"`
	TargetWithoutName bool    `json:"targetWithoutName"`
	TargetDelay       float64 `json:"targetDelay"`

	AttackEnabled bool    `json:"attackEnabled"`
	AttackDelay   float64 `json:"attackDelay"`

	PickEnabled bool    `json:"pickEnabled"`
	PickDelay   float64 `json:"pickDelay"`

	AutoPotHPEnabled  bool                      `json:"autoPotHPEnabled"`
	AutoPotHPPercent  float64                   `json:"autoPotHPPercent"`
	AutoPotHPSlotVK   uintptr                   `json:"autoPotHPSlotVK"`
	AutoPotTPEnabled  bool                      `json:"autoPotTPEnabled"`
	AutoPotTPPercent  float64                   `json:"autoPotTPPercent"`
	AutoPotTPSlotVK   uintptr                   `json:"autoPotTPSlotVK"`
	EmergencySkills   []WebEmergencySkillConfig `json:"emergencySkills"`
	AssistPanicTarget bool                      `json:"assistPanicTarget"`

	// Skills is the established Attacker/normal profile. SupportSkills is a
	// separate 1–0 profile selected only by Target Until Dead > Support, so
	// switching roles never overwrites an existing working attacker setup.
	Skills        []WebSkillConfig        `json:"skills"`
	SupportSkills []WebSupportSkillConfig `json:"supportSkills"`
}

// usesTargetNameFilter is deliberately false for Target without name even
// when a previous filtered profile left names in the text box. This mode is a
// key-only target loop: it must neither need a Target ROI nor hold skills for
// target-name OCR.
func usesTargetNameFilter(cfg WebBotConfig) bool {
	withoutName := cfg.TargetWithoutName || strings.EqualFold(strings.TrimSpace(cfg.TargetNameFilterMode), "none")
	return cfg.TargetUntilDeadEnabled ||
		(cfg.TargetEnabled && !withoutName && strings.TrimSpace(cfg.TargetUntilDeadCharacterName) != "")
}

func requiresTargetNameWhitelist(cfg WebBotConfig) bool {
	withoutName := cfg.TargetWithoutName || strings.EqualFold(strings.TrimSpace(cfg.TargetNameFilterMode), "none")
	usesNameList := cfg.TargetUntilDeadEnabled || (cfg.TargetEnabled && !withoutName)
	return usesNameList &&
		normalizeTargetNameFilterMode(cfg.TargetNameFilterMode) == targetNameFilterModeWhitelist &&
		strings.TrimSpace(cfg.TargetUntilDeadCharacterName) == ""
}

type WebEmergencySkillConfig struct {
	Enabled     bool    `json:"enabled"`
	VK          uintptr `json:"vk"`
	NeedsTarget bool    `json:"needsTarget"`
}

type WebSkillConfig struct {
	Name    string  `json:"name"`
	VK      uintptr `json:"vk"`
	Enabled bool    `json:"enabled"`
	Delay   float64 `json:"delay"`
}

type WebSupportSkillConfig struct {
	Name       string  `json:"name"`
	VK         uintptr `json:"vk"`
	Enabled    bool    `json:"enabled"`
	Delay      float64 `json:"delay"`
	WithTarget bool    `json:"withTarget"`
}

func validateAutoResurrectConfig(cfg WebBotConfig) string {
	if !cfg.AutoResurrectEnabled {
		return ""
	}
	if !cfg.AutoPauseDeathEnabled {
		return "Enable Auto Pause on Death before enabling Auto Resu."
	}
	if !LoadDeathROI().Selected {
		return "Select the full death dialog area before enabling Auto Resu."
	}
	if cfg.AutoPotHPPercent <= 0 {
		return "Set an HP threshold before enabling Auto Resu. HP Pot may remain unchecked."
	}
	if !LoadStatusROI().Selected {
		return "Select the HP / TP status area before enabling Auto Resu."
	}
	return ""
}

func validateAssistSkillConfig(cfg WebBotConfig) string {
	if cfg.AssistSkillVK == 0 {
		return ""
	}
	if cfg.AssistSkillDelay <= 0 {
		return "Enter an Assist skill interval greater than zero."
	}
	return ""
}

func validateNumericConfig(cfg WebBotConfig) string {
	validPositive := func(value float64, label string) string {
		if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
			return label + " must be a number greater than zero."
		}
		return ""
	}
	validPercent := func(value float64, label string) string {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 1 || value > 100 {
			return label + " must be a number from 1 to 100."
		}
		return ""
	}

	if cfg.AutoPotHPEnabled || (cfg.AutoPauseDeathEnabled && cfg.AutoPotHPPercent > 0) {
		if message := validPercent(cfg.AutoPotHPPercent, "HP Pot threshold"); message != "" {
			return message
		}
	}
	if cfg.AutoPotTPEnabled {
		if message := validPercent(cfg.AutoPotTPPercent, "TP Pot threshold"); message != "" {
			return message
		}
	}
	if cfg.AssistSkillVK != 0 {
		if message := validPositive(cfg.AssistSkillDelay, "Assist skill interval"); message != "" {
			return message
		}
	}
	if cfg.TargetEnabled {
		if message := validPositive(cfg.TargetDelay, "Target interval"); message != "" {
			return message
		}
	}
	if cfg.AttackEnabled && !(cfg.TargetUntilDeadEnabled && cfg.TargetUntilDeadSupport) {
		if message := validPositive(cfg.AttackDelay, "Attack interval"); message != "" {
			return message
		}
	}
	if cfg.PickEnabled {
		if message := validPositive(cfg.PickDelay, "Pick interval"); message != "" {
			return message
		}
	}
	activeSkills := cfg.Skills
	if cfg.TargetUntilDeadEnabled && cfg.TargetUntilDeadSupport {
		for _, skill := range cfg.SupportSkills {
			if skill.Enabled {
				if skill.VK < 0x30 || skill.VK > 0x39 {
					return "Support Skills may use only 1 through 0."
				}
				if message := validPositive(skill.Delay, "Support Skill "+skill.Name+" interval"); message != "" {
					return message
				}
			}
		}
		return ""
	}
	for _, skill := range activeSkills {
		if skill.Enabled {
			if message := validPositive(skill.Delay, "Skill "+skill.Name+" interval"); message != "" {
				return message
			}
		}
	}
	return ""
}

func validateEmergencySkillsConfig(cfg WebBotConfig) string {
	hasEnabled := false
	hasTargetRequired := false
	for _, slot := range cfg.EmergencySkills {
		if slot.Enabled {
			hasEnabled = true
			if slot.NeedsTarget {
				hasTargetRequired = true
			}
		}
	}
	if !hasEnabled {
		return ""
	}
	if !cfg.AutoPotHPEnabled || cfg.AutoPotHPPercent <= 0 {
		return "Enable HP Pot and set its percentage before enabling Emergency Skill."
	}
	if !LoadStatusROI().Selected {
		return "Select the HP / TP status area before enabling Emergency Skill."
	}
	if hasTargetRequired && !LoadTargetROI().Selected {
		return "Select the target name and red HP bar before enabling Need Target Emergency Skill."
	}
	return ""
}

// ============================================================
// AUTO ACCEPT
// ============================================================

type AutoAcceptController struct {
	mu      sync.RWMutex
	enabled bool
}

func NewAutoAcceptController(
	enabled bool,
) *AutoAcceptController {

	return &AutoAcceptController{
		enabled: enabled,
	}
}

func (a *AutoAcceptController) IsEnabled() bool {

	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.enabled
}

func (a *AutoAcceptController) SetEnabled(
	enabled bool,
) {

	a.mu.Lock()
	defer a.mu.Unlock()

	a.enabled = enabled
}

// ============================================================
// WEB UI
// ============================================================

// newWebUIMux keeps every existing web route in one handler. The desktop shell
// proxies its internal WebView2 requests to this mux, so the UI and bot API
// retain the same behaviour without opening Firefox or another browser.
func newWebUIMux(
	runtimeManager *RuntimeManager,
) http.Handler {

	mux := http.NewServeMux()
	var exitOnce sync.Once

	mux.HandleFunc(
		"/favicon.png",
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			_, _ = w.Write(katoolsFaviconPNG)
		},
	)

	// ========================================================
	// HOME
	// ========================================================

	mux.HandleFunc(
		"/",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {

			w.Header().Set(
				"Content-Type",
				"text/html; charset=utf-8",
			)

			fmt.Fprint(
				w,
				webUIHTML,
			)
		},
	)

	// ========================================================
	// WINDOWS
	// ========================================================

	mux.HandleFunc(
		"/api/windows",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {

			if r.Method != http.MethodGet {

				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)

				return
			}

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			json.NewEncoder(w).Encode(
				map[string]interface{}{
					"windows": enumerateWindows(),
				},
			)
		},
	)

	// ========================================================
	// STATUS
	// ========================================================

	mux.HandleFunc(
		"/api/status",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {

			if r.Method != http.MethodGet {

				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)

				return
			}

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			json.NewEncoder(w).Encode(
				runtimeManager.Status(),
			)
		},
	)

	// Closing the local browser UI ends the complete KaTools process rather
	// than leaving a hidden bot/web server behind.
	mux.HandleFunc("/api/exit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		exitOnce.Do(func() {
			go func() {
				// Let the unload beacon leave the browser before stopping its
				// receiving HTTP server.
				time.Sleep(150 * time.Millisecond)
				runtimeManager.Stop()
				os.Exit(0)
			}()
		})
	})

	// ========================================================
	// PARTY ROI
	//
	// GET  = read current ROI
	// POST = update ROI
	// ========================================================

	mux.HandleFunc(
		"/api/party-roi",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {

			switch r.Method {

			case http.MethodGet:

				cfg := ocrworker.LoadPartyROI()

				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				json.NewEncoder(w).Encode(
					map[string]interface{}{
						"success":  true,
						"x":        cfg.X,
						"y":        cfg.Y,
						"w":        cfg.Width,
						"h":        cfg.Height,
						"selected": cfg.Selected,
					},
				)

				return

			case http.MethodPost:

				handlePartyROIUpdate(
					w,
					r,
				)

				return

			default:

				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)

				return
			}
		},
	)

	// Preview PNG dari area terakhir yang dipilih pada native picker.
	mux.HandleFunc(
		"/api/party-roi/preview",
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			preview, ok := getPartyROIPreview()
			if !ok {
				http.Error(w, "no selected-area preview yet", http.StatusNotFound)
				return
			}

			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(preview)
		},
	)

	// ========================================================
	// PARTY ROI PICKER
	// ========================================================

	mux.HandleFunc(
		"/api/party-roi/picker",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {

			if r.Method != http.MethodPost {

				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)

				return
			}

			var request struct {
				HWND string `json:"hwnd"`
			}

			if err :=
				json.NewDecoder(
					r.Body,
				).Decode(&request); err != nil {

				writeJSONError(
					w,
					"Invalid JSON: "+err.Error(),
				)

				return
			}

			hwnd, err :=
				parseHWND(request.HWND)

			if err != nil {

				writeJSONError(
					w,
					fmt.Sprintf(
						"Invalid HWND: %v",
						err,
					),
				)

				return
			}

			// ------------------------------------------------
			// Open picker
			// ------------------------------------------------

			if err :=
				openPartyROIPicker(
					windows.Handle(hwnd),
				); err != nil {

				writeJSONError(
					w,
					err.Error(),
				)

				return
			}

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			json.NewEncoder(w).Encode(
				map[string]interface{}{
					"success": true,
					"message": "Party ROI picker opened.",
				},
			)
		},
	)

	mux.HandleFunc("/api/party-roi/reset", func(w http.ResponseWriter, r *http.Request) {
		handleROIReset(w, r, "party")
	})

	// ========================================================
	// STATUS HP / TP ROI
	// ========================================================

	mux.HandleFunc("/api/status-roi", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		roi := LoadStatusROI()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true, "selected": roi.Selected,
			"x": roi.X, "y": roi.Y, "w": roi.Width, "h": roi.Height,
		})
	})

	mux.HandleFunc("/api/status-roi/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		preview, ok := getStatusROIPreview()
		if !ok {
			http.Error(w, "no selected-area preview yet", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(preview)
	})

	mux.HandleFunc("/api/status-roi/picker", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			HWND string `json:"hwnd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error())
			return
		}
		hwnd, err := parseHWND(request.HWND)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("Invalid HWND: %v", err))
			return
		}
		if err := openStatusROIPicker(windows.Handle(hwnd)); err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	})

	mux.HandleFunc("/api/status-roi/reset", func(w http.ResponseWriter, r *http.Request) {
		handleROIReset(w, r, "status")
	})

	// ========================================================
	// DEATH DIALOG ROI
	// ========================================================

	mux.HandleFunc("/api/death-roi", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		roi := LoadDeathROI()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true, "selected": roi.Selected,
			"x": roi.X, "y": roi.Y, "w": roi.Width, "h": roi.Height,
		})
	})

	mux.HandleFunc("/api/death-roi/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		preview, ok := getDeathROIPreview()
		if !ok {
			http.Error(w, "no selected-area preview yet", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(preview)
	})

	mux.HandleFunc("/api/death-roi/picker", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			HWND string `json:"hwnd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error())
			return
		}
		hwnd, err := parseHWND(request.HWND)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("Invalid HWND: %v", err))
			return
		}
		if err := openDeathROIPicker(windows.Handle(hwnd)); err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	})

	mux.HandleFunc("/api/death-roi/reset", func(w http.ResponseWriter, r *http.Request) {
		handleROIReset(w, r, "death")
	})

	// ========================================================
	// TARGET MONSTER HP BAR ROI
	// ========================================================

	mux.HandleFunc("/api/target-roi", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		roi := LoadTargetROI()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true, "selected": roi.Selected,
			"x": roi.X, "y": roi.Y, "w": roi.Width, "h": roi.Height,
		})
	})

	mux.HandleFunc("/api/target-roi/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		preview, ok := getTargetROIPreview()
		if !ok {
			http.Error(w, "no selected-area preview yet", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(preview)
	})

	mux.HandleFunc("/api/target-roi/picker", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			HWND string `json:"hwnd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error())
			return
		}
		hwnd, err := parseHWND(request.HWND)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("Invalid HWND: %v", err))
			return
		}
		if err := openTargetROIPicker(windows.Handle(hwnd)); err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	})

	mux.HandleFunc("/api/target-roi/reset", func(w http.ResponseWriter, r *http.Request) {
		handleROIReset(w, r, "target")
	})

	// ========================================================
	// LIVE CONFIG
	// ========================================================

	// Named character profiles are separate from the legacy single config.
	mux.HandleFunc("/api/config/profiles", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		profiles, err := listWebConfigProfiles()
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		_, legacyFound, err := loadSavedWebConfig()
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":     true,
			"profiles":    profiles,
			"legacyFound": legacyFound,
		})
	})

	mux.HandleFunc("/api/config/profile", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			name := r.URL.Query().Get("name")
			cfg, found, err := loadProfileWebConfig(name)
			if err != nil {
				writeJSONError(w, err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"found":   found,
				"name":    name,
				"config":  cfg,
			})

		case http.MethodPost:
			var request struct {
				Name   string       `json:"name"`
				Config WebBotConfig `json:"config"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				writeJSONError(w, "Invalid JSON: "+err.Error())
				return
			}
			if message := validateNumericConfig(request.Config); message != "" {
				writeJSONError(w, message)
				return
			}
			name, err := saveProfileWebConfig(request.Name, request.Config)
			if err != nil {
				writeJSONError(w, err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"name":    name,
			})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc(
		"/api/config",
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				cfg, found, err := loadSavedWebConfig()
				if err != nil {
					writeJSONError(w, err.Error())
					return
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
					"found":   found,
					"config":  cfg,
				})
				return
			}

			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var cfg WebBotConfig
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				writeJSONError(w, "Invalid JSON: "+err.Error())
				return
			}
			if message := validateNumericConfig(cfg); message != "" {
				writeJSONError(w, message)
				return
			}

			if cfg.AutoAcceptEnabled && !ocrworker.LoadPartyROI().Selected {
				writeJSONError(w, "Select Party OCR area before enabling Auto Accept Party.")
				return
			}
			if (cfg.AutoPotHPEnabled || cfg.AutoPotTPEnabled) && !LoadStatusROI().Selected {
				writeJSONError(w, "Select the HP / TP status area before enabling Auto Potion.")
				return
			}
			if cfg.AutoPauseDeathEnabled && !LoadDeathROI().Selected {
				writeJSONError(w, "Select the death dialog area before enabling Auto Pause on Death.")
				return
			}
			if message := validateAutoResurrectConfig(cfg); message != "" {
				writeJSONError(w, message)
				return
			}
			if message := validateAssistSkillConfig(cfg); message != "" {
				writeJSONError(w, message)
				return
			}
			if message := validateEmergencySkillsConfig(cfg); message != "" {
				writeJSONError(w, message)
				return
			}
			if usesTargetNameFilter(cfg) && !LoadTargetROI().Selected {
				writeJSONError(w, "Select the target name and HP bar before using Target Name Filter or Target Until Dead.")
				return
			}
			if requiresTargetNameWhitelist(cfg) {
				writeJSONError(w, "Enter one or more whitelist target names before enabling Target or Target Until Dead.")
				return
			}
			if cfg.TargetUntilDeadEnabled && strings.TrimSpace(cfg.TargetUntilDeadCharacterName) == "" {
				writeJSONError(w, "Enter one or more target names before enabling Target Until Dead.")
				return
			}

			if runtimeManager.IsRunning() {
				if err := runtimeManager.UpdateConfig(cfg); err != nil {
					writeJSONError(w, err.Error())
					return
				}
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "Live config applied.",
			})
		},
	)

	// Config persistence is deliberately explicit: changing a delay should
	// immediately update a running bot, but it must not overwrite the saved
	// preset until the user presses SAVE CONFIG in the UI.
	mux.HandleFunc(
		"/api/config/save",
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var cfg WebBotConfig
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				writeJSONError(w, "Invalid JSON: "+err.Error())
				return
			}
			if message := validateNumericConfig(cfg); message != "" {
				writeJSONError(w, message)
				return
			}
			if err := saveWebConfig(cfg); err != nil {
				writeJSONError(w, err.Error())
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "Config saved.",
			})
		},
	)

	// ========================================================
	// START
	// ========================================================

	mux.HandleFunc(
		"/api/start",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {

			if r.Method != http.MethodPost {

				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)

				return
			}

			var cfg WebBotConfig

			if err :=
				json.NewDecoder(
					r.Body,
				).Decode(&cfg); err != nil {

				writeJSONError(
					w,
					"Invalid JSON: "+err.Error(),
				)

				return
			}

			if message := validateNumericConfig(cfg); message != "" {

				writeJSONError(w, message)

				return
			}

			if cfg.AutoAcceptEnabled && !ocrworker.LoadPartyROI().Selected {
				writeJSONError(
					w,
					"Select Party OCR area before enabling Auto Accept Party.",
				)
				return
			}

			if (cfg.AutoPotHPEnabled || cfg.AutoPotTPEnabled) && !LoadStatusROI().Selected {
				writeJSONError(
					w,
					"Select the HP / TP status area before enabling Auto Potion.",
				)
				return
			}

			if cfg.AutoPauseDeathEnabled && !LoadDeathROI().Selected {
				writeJSONError(
					w,
					"Select the death dialog area before enabling Auto Pause on Death.",
				)
				return
			}

			if message := validateAutoResurrectConfig(cfg); message != "" {
				writeJSONError(w, message)
				return
			}

			if message := validateAssistSkillConfig(cfg); message != "" {
				writeJSONError(w, message)
				return
			}

			if message := validateEmergencySkillsConfig(cfg); message != "" {
				writeJSONError(w, message)
				return
			}

			if usesTargetNameFilter(cfg) && !LoadTargetROI().Selected {
				writeJSONError(
					w,
					"Select the target name and HP bar before using Target Name Filter or Target Until Dead.",
				)
				return
			}

			if requiresTargetNameWhitelist(cfg) {
				writeJSONError(
					w,
					"Enter one or more whitelist target names before enabling Target or Target Until Dead.",
				)
				return
			}

			if cfg.TargetUntilDeadEnabled && strings.TrimSpace(cfg.TargetUntilDeadCharacterName) == "" {
				writeJSONError(
					w,
					"Enter one or more target names before enabling Target Until Dead.",
				)
				return
			}

			if cfg.HWND == "" {

				writeJSONError(
					w,
					"Please select a target window first.",
				)

				return
			}

			hwnd, err :=
				parseHWND(cfg.HWND)

			if err != nil {

				writeJSONError(
					w,
					fmt.Sprintf(
						"Invalid HWND: %v",
						err,
					),
				)

				return
			}

			if err :=
				runtimeManager.Start(
					hwnd,
					cfg,
				); err != nil {

				writeJSONError(
					w,
					err.Error(),
				)

				return
			}

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			json.NewEncoder(w).Encode(
				map[string]interface{}{
					"success": true,
					"message": "KaTools started!",
				},
			)
		},
	)

	// ========================================================
	// STOP
	// ========================================================

	mux.HandleFunc(
		"/api/stop",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {

			if r.Method != http.MethodPost {

				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)

				return
			}

			runtimeManager.Stop()

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			json.NewEncoder(w).Encode(
				map[string]interface{}{
					"success": true,
					"message": "KaTools stopped!",
				},
			)
		},
	)

	return mux
}

// ============================================================
// PARTY ROI HANDLER
// ============================================================

func handlePartyROIUpdate(
	w http.ResponseWriter,
	r *http.Request,
) {

	var cfg PartyROIConfig

	if err :=
		json.NewDecoder(
			r.Body,
		).Decode(&cfg); err != nil {

		writeJSONError(
			w,
			"Invalid JSON: "+err.Error(),
		)

		return
	}

	if err := setPartyROIConfig(cfg); err != nil {

		writeJSONError(
			w,
			err.Error(),
		)

		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	json.NewEncoder(w).Encode(
		map[string]interface{}{
			"success": true,
			"message": "Party ROI updated.",
			"x":       cfg.X,
			"y":       cfg.Y,
			"w":       cfg.W,
			"h":       cfg.H,
		},
	)
}

// ============================================================
// BOT CONFIG
// ============================================================

func applyWebBotConfig(
	bot *BotController,
	autoAccept *AutoAcceptController,
	autoPot *AutoPotController,
	emergency *EmergencySkillController,
	deathPause *DeathPauseController,
	targetUntil *TargetUntilDeadController,
	cfg WebBotConfig,
) {

	autoAccept.SetEnabled(
		cfg.AutoAcceptEnabled,
	)

	autoPot.Update(cfg)
	emergency.Update(cfg)
	deathPause.Update(cfg.AutoPauseDeathEnabled, cfg.AutoResurrectEnabled)
	targetFilterEnabled := usesTargetNameFilter(cfg)
	supportMode := cfg.TargetUntilDeadEnabled && cfg.TargetUntilDeadSupport
	targetUntil.Update(
		cfg.TargetUntilDeadEnabled,
		targetFilterEnabled,
		supportMode,
		cfg.TargetUntilDeadCharacterName,
		cfg.TargetNameFilterMode,
	)
	bot.SetTargetActionFilterEnabled(targetFilterEnabled)
	bot.SetTargetActionReady(!targetFilterEnabled)
	bot.SetTargetPanelClear(false)

	bot.mu.Lock()

	bot.config.AssistEnabled =
		!cfg.TargetEnabled && !cfg.TargetUntilDeadEnabled && cfg.AssistSkillVK != 0

	bot.config.AssistSkillVK =
		cfg.AssistSkillVK

	bot.config.AssistSkillDelay =
		secondsToDuration(
			cfg.AssistSkillDelay,
		)

	bot.config.TargetEnabled =
		cfg.TargetEnabled && !cfg.TargetUntilDeadEnabled

	bot.config.TargetDelay =
		secondsToDuration(
			cfg.TargetDelay,
		)

	// R is an attacker action. Preserve its checkbox and saved value for the
	// Attacker profile, but never schedule it while the Support profile owns
	// Target Until Dead.
	bot.config.AttackEnabled =
		cfg.AttackEnabled && !supportMode

	bot.config.AttackDelay =
		secondsToDuration(
			cfg.AttackDelay,
		)

	bot.config.PickEnabled =
		cfg.PickEnabled

	bot.config.PickDelay =
		secondsToDuration(
			cfg.PickDelay,
		)

	bot.config.Skills =
		make(
			[]SkillConfig,
			0,
			len(cfg.Skills)+len(cfg.SupportSkills),
		)

	if supportMode {
		for _, skill := range cfg.SupportSkills {
			if skill.VK < 0x30 || skill.VK > 0x39 {
				continue
			}
			bot.config.Skills = append(
				bot.config.Skills,
				SkillConfig{
					Name:               skill.Name,
					VK:                 skill.VK,
					Enabled:            skill.Enabled,
					Delay:              secondsToDuration(skill.Delay),
					BypassTargetGate:   true,
					WaitForTargetClear: !skill.WithTarget,
				},
			)
		}
	} else {
		for _, skill := range cfg.Skills {

			bot.config.Skills =
				append(
					bot.config.Skills,
					SkillConfig{
						Name:    skill.Name,
						VK:      skill.VK,
						Enabled: skill.Enabled,
						Delay: secondsToDuration(
							skill.Delay,
						),
					},
				)
		}
	}

	bot.config.Enabled = true

	bot.mu.Unlock()
}

func secondsToDuration(
	seconds float64,
) time.Duration {

	return time.Duration(
		seconds *
			float64(time.Second),
	)
}

// ============================================================
// WINDOWS
// ============================================================

type WindowInfo struct {
	HWND  string `json:"hwnd"`
	Title string `json:"title"`
}

type PowerShellWindowInfo struct {
	HWND  uint64 `json:"HWND"`
	Title string `json:"Title"`
}

func enumerateWindows() []WindowInfo {

	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-Command",
		`
$windows = Get-Process |
Where-Object {
	$_.MainWindowHandle -ne 0 -and
	$_.MainWindowTitle -ne ""
} |
ForEach-Object {
	[PSCustomObject]@{
		HWND  = [Int64]$_.MainWindowHandle
		Title = $_.MainWindowTitle
	}
}

$json = @($windows) | ConvertTo-Json -Compress

[Convert]::ToBase64String(
	[System.Text.Encoding]::UTF8.GetBytes($json)
)
		`,
	)
	// PowerShell is only a short-lived helper for listing candidate game
	// windows. Keep its console hidden in the GUI release.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.Output()

	if err != nil {

		fmt.Println(
			"PowerShell error:",
			err,
		)

		return []WindowInfo{}
	}

	encoded :=
		strings.TrimSpace(
			string(output),
		)

	if encoded == "" {

		fmt.Println(
			"PowerShell returned empty output.",
		)

		return []WindowInfo{}
	}

	jsonBytes, err :=
		base64.StdEncoding.DecodeString(
			encoded,
		)

	if err != nil {

		fmt.Println(
			"Base64 decode error:",
			err,
		)

		return []WindowInfo{}
	}

	var windowsList []PowerShellWindowInfo

	err =
		json.Unmarshal(
			jsonBytes,
			&windowsList,
		)

	if err != nil {

		fmt.Println(
			"JSON parse error:",
			err,
		)

		return []WindowInfo{}
	}

	result :=
		make(
			[]WindowInfo,
			0,
			len(windowsList),
		)

	for _, item := range windowsList {

		result =
			append(
				result,
				WindowInfo{
					HWND: fmt.Sprintf(
						"0x%X",
						item.HWND,
					),

					Title: item.Title,
				},
			)
	}

	return result
}

func parseHWND(
	value string,
) (uintptr, error) {

	parsed, err :=
		strconv.ParseUint(
			value,
			0,
			64,
		)

	if err != nil {
		return 0, err
	}

	return uintptr(parsed), nil
}

func handleROIReset(w http.ResponseWriter, r *http.Request, kind string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var err error
	switch kind {
	case "party":
		err = ocrworker.ResetPickedPartyROI()
	case "status":
		err = ResetPickedStatusROI()
	case "death":
		err = ResetPickedDeathROI()
	case "target":
		err = ResetPickedTargetROI()
	default:
		writeJSONError(w, "Unknown ROI kind")
		return
	}
	if err != nil {
		writeJSONError(w, "Failed to reset selected area: "+err.Error())
		return
	}

	clearROIPreview(kind)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func writeJSONError(
	w http.ResponseWriter,
	message string,
) {

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	json.NewEncoder(w).Encode(
		map[string]interface{}{
			"success": false,
			"message": message,
		},
	)
}

func openBrowser(
	url string,
) {

	switch runtime.GOOS {

	case "windows":
		if openFirefox(url) {
			return
		}

		exec.Command(
			"cmd",
			"/c",
			"start",
			"",
			url,
		).Start()

	case "darwin":

		exec.Command(
			"open",
			url,
		).Start()

	default:

		exec.Command(
			"xdg-open",
			url,
		).Start()
	}
}

// openFirefox launches the KaTools UI in Firefox without consulting Windows'
// default-browser setting. The fallback in openBrowser keeps the UI reachable
// on computers where Firefox is not installed.
func openFirefox(url string) bool {
	candidates := make([]string, 0, 4)
	if firefoxPath, err := exec.LookPath("firefox.exe"); err == nil {
		candidates = append(candidates, firefoxPath)
	}

	for _, baseDir := range []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("LOCALAPPDATA"),
	} {
		if baseDir == "" {
			continue
		}
		candidates = append(candidates, filepath.Join(baseDir, "Mozilla Firefox", "firefox.exe"))
	}

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err != nil || info.IsDir() {
			continue
		}
		// -new-window consumes the next parameter as its URL. Supplying Firefox's
		// size flags before it made Firefox open "720" as a URL (0.0.2.208).
		if err := exec.Command(candidate, "-new-window", url).Start(); err == nil {
			return true
		}
	}

	return false
}
