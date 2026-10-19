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
:root {
  --bg-base: #0d0f14;
  --bg-surface: #161a24;
  --bg-elevated: #1e2330;
  --bg-card: #1a1f2e;
  --border-subtle: #2a3040;
  --border-active: #3d4660;
  --text-primary: #e8eaf0;
  --text-secondary: #8b92a8;
  --text-muted: #5a6278;
  --accent-cyan: #22d3ee;
  --accent-cyan-dim: #0e7490;
  --accent-green: #34d399;
  --accent-green-dim: #065f46;
  --accent-red: #f87171;
  --accent-red-dim: #7f1d1d;
  --accent-amber: #fbbf24;
  --accent-amber-dim: #78350f;
  --accent-blue: #60a5fa;
  --accent-blue-dim: #1e3a5f;
  --radius-sm: 6px;
  --radius-md: 10px;
  --radius-lg: 14px;
  --shadow-card: 0 2px 8px rgba(0,0,0,0.35);
  --shadow-glow-cyan: 0 0 16px rgba(34,211,238,0.25);
  --shadow-glow-green: 0 0 16px rgba(52,211,153,0.25);
  --transition: 0.18s ease;
}

*,*::before,*::after { box-sizing: border-box; }

html { font-size: 14px; }

body {
  font-family: system-ui, -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  background: var(--bg-base);
  color: var(--text-primary);
  margin: 0;
  padding: 16px;
  line-height: 1.5;
  -webkit-font-smoothing: antialiased;
}

body.light-theme {
  --bg-base: #f4f7fb;
  --bg-surface: #ffffff;
  --bg-elevated: #e8edf5;
  --bg-card: #f8faff;
  --border-subtle: #cbd5e1;
  --border-active: #94a3b8;
  --text-primary: #172033;
  --text-secondary: #475569;
  --text-muted: #64748b;
  --accent-cyan: #0891b2;
  --accent-cyan-dim: #c8f1f7;
  --accent-green: #15803d;
  --accent-green-dim: #d9f7e2;
  --accent-red: #dc2626;
  --accent-red-dim: #fee2e2;
  --accent-amber: #b45309;
  --accent-amber-dim: #fef3c7;
  --accent-blue: #2563eb;
  --accent-blue-dim: #dbeafe;
  --shadow-card: 0 2px 8px rgba(15, 23, 42, 0.10);
}

.container {
  width: min(1120px, 100%);
  max-width: 100%;
  margin: 0 auto;
}

.app-header {
  position: sticky;
  top: 0;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--border-subtle);
  background: var(--bg-base);
}

.app-title {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 0;
  font-size: 20px;
  font-weight: 700;
  letter-spacing: -0.3px;
  color: var(--text-primary);
}

.app-title-icon {
  width: 28px;
  height: 28px;
  background: linear-gradient(135deg, var(--accent-cyan), var(--accent-blue));
  border-radius: 7px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  font-weight: 900;
  color: #000;
  flex-shrink: 0;
}

.tab-bar {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}

.tab-button {
  width: auto;
  margin: 0;
  border: 0;
  padding: 7px 16px;
  background: transparent;
  color: var(--text-secondary);
  font-weight: 600;
  font-size: 13px;
  cursor: pointer;
  border-radius: var(--radius-sm);
  transition: var(--transition);
  letter-spacing: 0.2px;
}

.tab-button:hover { color: var(--text-primary); background: var(--bg-elevated); }
.tab-button.active { background: var(--bg-elevated); color: var(--accent-cyan); }

.theme-bar {
  display: flex;
  justify-content: flex-end;
  margin: -8px 0 14px;
}
.theme-picker {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 600;
}
.theme-picker select {
  width: 150px;
  padding: 7px 30px 7px 10px;
  font-size: 12px;
}

.tab-panel { display: none; }
.tab-panel.active { display: block; }

.scroll-nav {
  position: fixed;
  right: 14px;
  top: 50%;
  z-index: 40;
  display: flex;
  flex-direction: column;
  gap: 7px;
  transform: translateY(-50%);
}
.scroll-nav-button {
  width: 36px;
  height: 36px;
  margin: 0;
  padding: 0;
  border: 1px solid var(--border-active);
  border-radius: 999px;
  background: var(--bg-elevated);
  color: var(--accent-cyan);
  box-shadow: var(--shadow-card);
  font-size: 20px;
  line-height: 1;
  cursor: pointer;
  transition: var(--transition);
}
.scroll-nav-button:hover:not(:disabled) {
  border-color: var(--accent-cyan);
  box-shadow: var(--shadow-glow-cyan);
  transform: scale(1.06);
}
.scroll-nav-button:disabled {
  opacity: 0.28;
  cursor: default;
}

.section {
  margin-bottom: 20px;
}

h2 {
  margin: 0 0 10px 0;
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.8px;
  color: var(--text-muted);
}

/* ---- Status Banner ---- */
.status {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  border-radius: var(--radius-md);
  margin-bottom: 20px;
  font-size: 14px;
  font-weight: 600;
  border: 1px solid transparent;
  transition: var(--transition);
}

.status-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
}

.status.stopped {
  background: linear-gradient(135deg, #1c0a0a, #2d1010);
  border-color: #3d1515;
  color: #fca5a5;
}
.status.stopped .status-dot { background: var(--accent-red); }

.status.running {
  background: linear-gradient(135deg, #051a10, #0a2e1e);
  border-color: #14532d;
  color: #6ee7b7;
}
.status.running .status-dot {
  background: var(--accent-green);
  box-shadow: 0 0 8px var(--accent-green);
  animation: pulse-dot 2s ease-in-out infinite;
}
.status.starting {
  background: linear-gradient(135deg, #051a10, #0a2e1e);
  border-color: #14532d;
  color: #6ee7b7;
}
.status.starting .status-dot {
  background: var(--accent-green);
  animation: blink-dot 0.6s ease-in-out infinite;
}

.status.paused {
  background: linear-gradient(135deg, #1a0e00, #2d1a00);
  border-color: #78350f;
  color: #fcd34d;
}
.status.paused .status-dot { background: var(--accent-amber); animation: blink-dot 1s ease-in-out infinite; }

@keyframes pulse-dot {
  0%, 100% { opacity: 1; transform: scale(1); }
  50% { opacity: 0.7; transform: scale(1.2); }
}
@keyframes blink-dot {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}

/* ---- Window Panel ---- */
.window-panel {
  background: var(--bg-surface);
  padding: 12px;
  border-radius: var(--radius-md);
  border: 1px solid var(--border-subtle);
  margin-bottom: 20px;
  box-shadow: var(--shadow-card);
}

.window-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
}

.window-select {
  width: 100%;
  padding: 9px 12px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-active);
  background: var(--bg-base);
  color: var(--text-primary);
  font-size: 13px;
  outline: none;
  transition: var(--transition);
  cursor: pointer;
  appearance: none;
  -webkit-appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%238b92a8' stroke-width='2'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 10px center;
  padding-right: 32px;
}

.window-select:focus { border-color: var(--accent-cyan); box-shadow: 0 0 0 2px rgba(34,211,238,0.15); }

.refresh-button {
  width: auto;
  margin-top: 0;
  padding: 9px 14px;
  background: var(--bg-elevated);
  border: 1px solid var(--border-active);
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.5px;
  border-radius: var(--radius-sm);
  transition: var(--transition);
  cursor: pointer;
  white-space: nowrap;
}
.refresh-button:hover { background: var(--border-active); color: var(--text-primary); }
.refresh-button.loading { cursor: progress; opacity: 0.7; }
.refresh-button.loading::before {
  content: "";
  display: inline-block;
  width: 11px;
  height: 11px;
  margin-right: 6px;
  vertical-align: -1px;
  border: 2px solid rgba(255,255,255,0.25);
  border-top-color: #fff;
  border-radius: 50%;
  animation: refresh-spin 0.7s linear infinite;
}
@keyframes refresh-spin { to { transform: rotate(360deg); } }

.window-info {
  margin-top: 8px;
  color: var(--text-muted);
  font-size: 12px;
}

.game-launch-folder {
  overflow-wrap: anywhere;
  margin-bottom: 8px;
  color: var(--text-secondary);
}

.game-launch-actions {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 8px;
}

.game-launch-actions button {
  margin: 0;
  white-space: nowrap;
}

#chooseGameFolderButton {
  background: var(--accent-blue-dim);
  border: 1px solid var(--accent-blue);
  color: var(--accent-blue);
}

#chooseGameFolderButton:hover { background: rgba(96,165,250,0.18); }

#launchGameButton {
  background: var(--accent-green-dim);
  border: 1px solid var(--accent-green);
  color: var(--accent-green);
}

#launchGameButton:hover { background: rgba(52,211,153,0.18); }

#gameExecutableSelect {
  color: var(--accent-cyan);
  font-weight: 600;
}

#gameExecutableSelect:disabled { color: var(--text-muted); }
#gameExecutableSelect option { background: var(--bg-base); color: var(--text-primary); }

.auto-login-enable {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-bottom: 12px;
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 600;
}

.auto-login-enable input { accent-color: var(--accent-cyan); }
.auto-login-key {
  width: 46px;
  min-height: 30px;
  margin-left: 4px;
  padding: 3px 7px;
  text-align: center;
  text-transform: uppercase;
}

/* ---- Cards ---- */
.panel-card {
  background: var(--bg-surface);
  border: 1px solid var(--border-subtle);
  padding: 14px;
  border-radius: var(--radius-md);
  margin-bottom: 12px;
  box-shadow: var(--shadow-card);
}

.actions-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  margin-bottom: 12px;
}

.action-card {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  background: var(--bg-card);
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
  transition: var(--transition);
}

.action-card:hover { border-color: var(--border-active); }
.action-card label { min-width: 0; font-size: 13px; font-weight: 500; color: var(--text-primary); overflow-wrap: anywhere; }
.party-invite-controls { min-width: 0; display: flex; flex-direction: column; gap: 5px; }
.party-invite-actions { display: flex; gap: 10px; }
.party-invite-actions label { display: inline-flex; align-items: center; gap: 4px; font-size: 11px; font-weight: 500; color: var(--text-secondary); }
.party-invite-actions input { accent-color: var(--accent-cyan); }

/* Hidden state rules */
#attackActionCard[hidden], #pickActionCard[hidden], #normalFunctionSkillsSection[hidden],
.click-only-skill[hidden], #autoPotClickHint[hidden], #autoPotHPSlot[hidden],
#autoPotTPSlot[hidden], #keyboardActionsGrid[hidden], #targetPanelAreaSection[hidden],
#clickTargetBehavior[hidden], #clickWhitelistNamesSettings[hidden],
#partyROIInfo[hidden], #partyROIPreview[hidden], #deathROIInfo[hidden],
#deathROIPreview[hidden], .click-target-toggle[hidden], .skill-delay[hidden],
.click-area-controls[hidden], #pickDelay[hidden] {
  display: none !important;
}

/* A row defines display:grid, which otherwise overrides the browser's
   built-in [hidden] rule. Keep Click-only rows out of Keyboard Method. */
.roi-picker-row[hidden] { display: none !important; }

/* ---- Inputs ---- */
input[type="checkbox"] {
  width: 16px;
  height: 16px;
  margin: 0;
  cursor: pointer;
  accent-color: var(--accent-cyan);
}

input[type="text"].numeric-input {
  width: 100%;
  padding: 6px 8px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-active);
  background: var(--bg-base);
  color: var(--text-primary);
  font-size: 13px;
  outline: none;
  transition: var(--transition);
}
input[type="text"].numeric-input:focus { border-color: var(--accent-cyan); box-shadow: 0 0 0 2px rgba(34,211,238,0.12); }

select {
  width: 100%;
  padding: 6px 8px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-active);
  background: var(--bg-base);
  color: var(--text-primary);
  font-size: 13px;
  outline: none;
  transition: var(--transition);
  cursor: pointer;
  appearance: none;
  -webkit-appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%238b92a8' stroke-width='2'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 8px center;
  padding-right: 28px;
}
select:focus { border-color: var(--accent-cyan); box-shadow: 0 0 0 2px rgba(34,211,238,0.12); }

/* ---- Skills Grid ---- */
.skills-grid {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 6px;
}

.skill-card {
  display: grid;
  grid-template-columns: auto auto 52px;
  align-items: center;
  gap: 5px;
  background: var(--bg-card);
  padding: 7px 8px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
  transition: var(--transition);
}
.skill-card:hover { border-color: var(--border-active); }

.click-whitelist-mode .skill-card {
  min-width: 0;
  grid-template-columns: auto minmax(0, 1fr) auto;
  grid-template-areas:
    "check name target"
    ". delay delay"
    "controls controls controls";
  gap: 7px;
}
.click-whitelist-mode .skills-grid { grid-template-columns: repeat(auto-fit, minmax(min(260px, 100%), 1fr)); }
.click-whitelist-mode .skill-check { grid-area: check; }
.click-whitelist-mode .skill-name { grid-area: name; }
.click-whitelist-mode .click-target-toggle { grid-area: target; }
.click-whitelist-mode .skill-delay { grid-area: delay; width: 100% !important; }
.click-whitelist-mode .click-area-controls { grid-area: controls; }

.click-target-toggle {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  white-space: nowrap;
  font-size: 10px;
  color: var(--accent-amber);
  font-weight: 600;
}

.skill-name { font-size: 13px; font-weight: 700; align-self: center; color: var(--text-primary); }
.skill-check { align-self: center; }
.skill-delay { width: 52px !important; min-width: 0; padding: 4px 6px !important; font-size: 12px !important; }

/* ---- Target Mode ---- */
.target-mode-card {
  background: var(--bg-card);
  padding: 12px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
  margin-bottom: 8px;
}
#keyboardActionsGrid .target-mode-card { grid-column: 1 / -1; }
.target-mode-title { font-size: 13px; font-weight: 700; margin-bottom: 8px; color: var(--text-primary); }
.target-mode-options { display: flex; flex-wrap: wrap; gap: 6px; }
.target-mode-option {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 10px;
  border: 1px solid var(--border-active);
  border-radius: var(--radius-sm);
  font-size: 12px;
  cursor: pointer;
  color: var(--text-secondary);
  transition: var(--transition);
  user-select: none;
}
.target-mode-option:hover { color: var(--text-primary); border-color: var(--accent-cyan); }
.target-mode-option input { accent-color: var(--accent-cyan); }
.target-mode-option input:checked + span,
.target-mode-option:has(input:checked) { color: var(--accent-cyan); border-color: var(--accent-cyan-dim); background: rgba(34,211,238,0.06); }

.target-mode-option[hidden] { display: none; }
.target-mode-settings {
  display: grid;
  grid-template-columns: minmax(100px, 1fr) auto;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
}
.target-mode-settings[hidden] { display: none; }
.target-mode-settings label { font-size: 12px; color: var(--text-secondary); }
.target-mode-settings .target-name-card { grid-column: 1 / -1; }

.target-name-card {
  display: grid;
  grid-template-columns: auto minmax(100px, 1fr);
  align-items: center;
  gap: 6px;
  background: var(--bg-card);
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
}
.target-name-card label { font-size: 12px; color: var(--text-secondary); }
.target-name-card input { width: 100%; min-width: 0; padding: 5px 8px; border-radius: var(--radius-sm); border: 1px solid var(--border-active); background: var(--bg-base); color: var(--text-primary); font-size: 12px; outline: none; }
.target-name-card input:focus { border-color: var(--accent-cyan); box-shadow: 0 0 0 2px rgba(34,211,238,0.12); }

/* ---- ROI & Picker ---- */
.roi-picker-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  background: var(--bg-card);
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
  margin-bottom: 8px;
}
.roi-picker-title { font-size: 13px; font-weight: 600; color: var(--text-primary); margin-bottom: 2px; }
.party-roi-info { font-size: 11px; color: var(--text-muted); margin-top: 3px; }

.roi-button-group { display: flex; gap: 5px; }
.party-picker-button {
  min-width: 60px;
  height: 30px;
  padding: 0 10px;
  margin: 0;
  background: var(--accent-blue-dim);
  border: 1px solid var(--accent-blue);
  border-radius: var(--radius-sm);
  font-size: 11px;
  font-weight: 700;
  color: var(--accent-blue);
  cursor: pointer;
  transition: var(--transition);
  letter-spacing: 0.3px;
}
.party-picker-button:hover { background: rgba(96,165,250,0.15); }
.roi-reset-button {
  min-width: 54px;
  background: var(--accent-red-dim);
  border-color: var(--accent-red);
  color: var(--accent-red);
}
.roi-reset-button:hover { background: rgba(248,113,113,0.15); }

.party-roi-preview {
  display: none;
  margin-top: 8px;
  max-width: 100%;
  max-height: 160px;
  object-fit: contain;
  background: var(--bg-base);
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
}

/* ---- Global Click Action Areas ---- */
.click-global-action-area {
  grid-template-columns: repeat(3, minmax(0, 1fr));
  align-items: start;
}
.click-popup-scan-card {
  background: var(--bg-card);
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
  min-width: 0;
}
.keyboard-popup-scan-area {
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-items: stretch;
}
.click-popup-scan-preview {
  width: 100%;
  max-height: 150px;
  object-fit: contain;
  background: var(--bg-base);
  margin-top: 6px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
}

/* ---- Pot Grid ---- */
.pot-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 8px;
}

.pot-card {
  display: grid;
  grid-template-columns: auto 1fr 72px 88px;
  align-items: center;
  gap: 8px;
  background: var(--bg-card);
  padding: 9px 10px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
}
.pot-card label { font-size: 13px; font-weight: 600; }

/* ---- Emergency ---- */
.emergency-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
}
.emergency-card {
  display: grid;
  grid-template-columns: auto minmax(64px, 1fr) auto;
  align-items: center;
  gap: 5px;
  background: var(--bg-card);
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
}
.emergency-card select { width: 100%; min-width: 0; padding: 5px 8px; border-radius: var(--radius-sm); border: 1px solid var(--border-active); background: var(--bg-base); color: var(--text-primary); font-size: 12px; outline: none; }
.emergency-target-label { display: flex; align-items: center; gap: 4px; font-size: 11px; color: var(--text-muted); }
.emergency-target-label input { accent-color: var(--accent-cyan); }
.emergency-target-label[hidden] { display: none !important; }
.emergency-panic-row { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-secondary); background: var(--bg-card); padding: 8px 10px; border-radius: var(--radius-sm); border: 1px solid var(--border-subtle); margin-top: 8px; }
.emergency-panic-row[hidden] { display: none !important; }
.emergency-panic-row input { accent-color: var(--accent-amber); }

/* ---- Click Area Controls ---- */
.click-area-controls {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  flex-wrap: wrap;
}
.click-area-status { font-size: 11px; color: var(--text-muted); }
.click-area-buttons { display: flex; gap: 4px; flex-wrap: wrap; }
.click-area-buttons button {
  width: auto;
  margin-top: 0;
  padding: 4px 8px;
  font-size: 10px;
  font-weight: 700;
  border-radius: var(--radius-sm);
  border: 1px solid;
  cursor: pointer;
  transition: var(--transition);
}
.click-area-buttons button[onclick^="openClickSkillPicker"],
.click-area-buttons button.chat-scan-area-button {
  background: var(--accent-blue-dim);
  color: var(--accent-blue);
  border-color: var(--accent-blue);
}
.click-area-buttons button[onclick^="openClickSkillPicker"]:hover,
.click-area-buttons button.chat-scan-area-button:hover { background: rgba(96,165,250,0.18); }

.click-area-buttons button[onclick^="reset"] {
  background: var(--accent-red-dim);
  color: var(--accent-red);
  border-color: var(--accent-red);
}
.click-area-buttons button[onclick^="reset"]:hover { background: rgba(248,113,113,0.15); }

/* Chat party invite is shared by every input method. Keep its controls in
   deliberate fields instead of letting a generic flex row scatter them. */
.chat-text-invite-controls {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(240px, 1.25fr) minmax(260px, 1.1fr) minmax(160px, 0.55fr);
  align-items: end;
  gap: 10px 14px;
  margin-top: 10px;
}
.chat-text-invite-field { min-width: 0; }
.chat-text-invite-field > label,
.chat-text-invite-field > .roi-picker-title {
  display: block;
  margin-bottom: 5px;
  font-size: 12px;
  color: var(--text-secondary);
}
.chat-text-invite-field input[type="text"],
.chat-text-invite-field input[type="number"] {
  width: 100%;
  box-sizing: border-box;
}
.chat-text-invite-field .click-area-controls {
  min-height: 30px;
}
.chat-text-invite-scan .click-area-controls,
.chat-text-invite-pointer .click-area-controls {
  justify-content: flex-start;
}
.chat-text-invite-scan .click-area-status,
.chat-text-invite-pointer .click-area-status { display: none; }

/* ---- Support Skills ---- */
.support-skills-grid { grid-template-columns: repeat(2, 1fr); }
.support-skill-card {
  display: grid;
  grid-template-columns: auto auto 52px minmax(0, 1fr);
  align-items: center;
  gap: 5px;
  background: var(--bg-card);
  padding: 7px 8px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
}
.support-with-target { display: flex; align-items: center; gap: 4px; font-size: 11px; white-space: nowrap; color: var(--text-muted); }

/* ---- Buttons ---- */
button {
  width: 100%;
  border: none;
  border-radius: var(--radius-md);
  color: white;
  font-size: 14px;
  font-weight: 700;
  cursor: pointer;
  margin-top: 16px;
  transition: var(--transition);
  letter-spacing: 0.3px;
}
button:disabled { opacity: 0.4; cursor: not-allowed; }
button:disabled:hover { transform: none; }

.bot-button {
  padding: 14px 20px;
  font-size: 15px;
  letter-spacing: 0.8px;
  border-radius: var(--radius-md);
  position: relative;
  overflow: hidden;
}
.bot-button::before {
  content: "";
  position: absolute;
  inset: 0;
  opacity: 0;
  transition: opacity 0.3s ease;
}
.bot-button.start {
  background: linear-gradient(135deg, #15803d, #16a34a, #22c55e);
  border: 1px solid #22c55e;
  box-shadow: 0 4px 16px rgba(34,197,94,0.25);
}
.bot-button.start:hover {
  transform: translateY(-1px);
  box-shadow: 0 6px 24px rgba(34,197,94,0.35);
}
.bot-button.stop {
  background: linear-gradient(135deg, #991b1b, #b91c1c, #dc2626);
  border: 1px solid #dc2626;
  box-shadow: 0 4px 16px rgba(220,38,38,0.25);
}
.bot-button.stop:hover {
  transform: translateY(-1px);
  box-shadow: 0 6px 24px rgba(220,38,38,0.35);
}

.config-actions {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  margin-top: 10px;
}
.config-actions button {
  margin-top: 0;
  padding: 10px;
  background: var(--bg-elevated);
  border: 1px solid var(--border-active);
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.5px;
  border-radius: var(--radius-sm);
}
.config-actions button:hover { background: var(--border-active); color: var(--text-primary); }

/* ---- Hints ---- */
.hint {
  color: var(--text-muted);
  font-size: 11px;
  margin-top: 6px;
  line-height: 1.5;
}

/* ---- Modal ---- */
.config-profile-modal {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: grid;
  place-items: center;
  padding: 18px;
  background: rgba(0,0,0,0.65);
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
}
.config-profile-modal[hidden] { display: none; }

.config-profile-dialog {
  width: min(100%, 360px);
  padding: 20px;
  border: 1px solid var(--border-active);
  border-radius: var(--radius-lg);
  background: var(--bg-surface);
  box-shadow: 0 24px 64px rgba(0,0,0,0.5);
}
.config-profile-dialog h2 { margin-bottom: 4px; font-size: 16px; color: var(--text-primary); }
.config-profile-dialog p { margin: 0 0 14px; color: var(--text-muted); font-size: 12px; }
.config-profile-dialog input,
.config-profile-dialog select {
  width: 100%;
  padding: 10px 12px;
  border: 1px solid var(--border-active);
  border-radius: var(--radius-sm);
  background: var(--bg-base);
  color: var(--text-primary);
  font-size: 13px;
  outline: none;
  transition: var(--transition);
  margin-bottom: 8px;
}
.config-profile-dialog input:focus,
.config-profile-dialog select:focus { border-color: var(--accent-cyan); box-shadow: 0 0 0 2px rgba(34,211,238,0.15); }
.config-profile-dialog select {
  appearance: none;
  -webkit-appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%238b92a8' stroke-width='2'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 12px center;
  padding-right: 36px;
}
.config-profile-dialog-actions { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-top: 14px; }
.config-profile-dialog-actions button {
  margin: 0;
  padding: 10px;
  background: var(--bg-elevated);
  border: 1px solid var(--border-active);
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 700;
  border-radius: var(--radius-sm);
}
.config-profile-dialog-actions button:last-child { background: var(--accent-green-dim); border-color: var(--accent-green); color: var(--accent-green); }

/* ---- Skill Live Status ---- */
#skillLiveApplyStatus { font-size: 11px; margin-top: 6px; }
#skillLiveApplyStatus.error-text { color: #fca5a5; }

/* ---- Input Method Section ---- */
#inputMethodSection h2 { margin-bottom: 8px; }
#clickTargetBehavior { margin-top: 6px; }

/* ---- Responsive ---- */
@media (max-width: 860px) {
  body { padding: 12px; }
  .app-header { flex-direction: column; align-items: flex-start; gap: 12px; }
  .actions-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .click-global-action-area { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .keyboard-popup-scan-area { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .click-popup-scan-card { min-width: 0; }
  .chat-text-invite-controls { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .skills-grid { grid-template-columns: repeat(3, 1fr); }
  .pot-grid { grid-template-columns: 1fr; }
  .emergency-grid { grid-template-columns: repeat(2, 1fr); }
  .support-skills-grid { grid-template-columns: 1fr; }
}
@media (max-width: 620px) {
  .theme-bar { justify-content: flex-start; }
  .theme-picker { width: 100%; justify-content: space-between; }
  .theme-picker select { width: min(220px, 65%); }
  .window-row,
  .roi-picker-row { grid-template-columns: 1fr; }
  .refresh-button,
  .roi-picker-row > .party-picker-button { width: 100%; }
  .actions-grid,
  .click-global-action-area,
  .keyboard-popup-scan-area,
  .chat-text-invite-controls { grid-template-columns: 1fr; }

  .game-launch-actions { grid-template-columns: 1fr; }
  .action-card { grid-template-columns: auto minmax(0, 1fr) auto; }
  .config-actions { grid-template-columns: 1fr; }
  .scroll-nav { right: 7px; }
  .scroll-nav-button { width: 32px; height: 32px; font-size: 18px; }
}
@media (max-width: 540px) {
  body { padding: 10px; }
  .tab-bar { width: 100%; }
  .tab-button { flex: 1 1 auto; }
  .skills-grid { grid-template-columns: repeat(2, 1fr); }
  .emergency-grid { grid-template-columns: 1fr; }
  .app-title { font-size: 17px; }
  .bot-button { padding: 12px 16px; font-size: 13px; }
}

/* ---- Click Input Mode Overrides ---- */
body.click-input-mode .party-action-card .roi-button-group { display: none; }
body.click-input-mode #partyROIInfo, body.click-input-mode #partyROIPreview,
body.click-input-mode #deathROIInfo, body.click-input-mode #deathROIPreview { display: none !important; }
.click-input-mode .emergency-slot { display: none; }
.click-input-mode .emergency-card { grid-template-columns: auto minmax(0, 1fr); }
</style>

</head>

<body>

<div class="container">

	<div class="app-header">
		<h1 class="app-title" id="appTitle">
			<span class="app-title-icon">K</span>
			KaTools
		</h1>

		<div class="tab-bar" role="tablist" aria-label="KaTools panels">
			<button type="button" class="tab-button active" data-tab="main" onclick="showTab('main')">Bot</button>
			<button type="button" class="tab-button" data-tab="emergency" onclick="showTab('emergency')">Emergency Skill</button>
			<button type="button" class="tab-button" data-tab="autoLogin" onclick="showTab('autoLogin')">Auto Login</button>
		</div>
	</div>

	<div class="theme-bar">
		<label class="theme-picker" for="themeSelect">
			<span>Theme</span>
			<select id="themeSelect" onchange="setTheme(this.value)" aria-label="Theme">
				<option value="dark">Dark Theme</option>
				<option value="light">White Theme</option>
			</select>
		</label>
	</div>

	<div id="mainTab" class="tab-panel active">

		<div
			id="status"
			class="status stopped">
			<span class="status-dot"></span>
			<span>● BOT STOPPED</span>
		</div>

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

	<div class="section" id="inputMethodSection">
		<h2>Input Method</h2>
		<div class="target-mode-options">
			<label class="target-mode-option"><input type="radio" name="targetInputMethod" value="keyboard" checked disabled> Keyboard Method</label>
			<label class="target-mode-option"><input type="radio" name="targetInputMethod" value="keypress" disabled> Keypress Method</label>
			<label class="target-mode-option"><input type="radio" name="targetInputMethod" value="click" disabled> Click Method</label>
		</div>
		<div id="clickTargetBehavior" class="target-mode-options" hidden>
			<label class="target-mode-option"><input type="radio" name="clickTargetBehavior" value="withoutName" checked> Without Name</label>
			<label class="target-mode-option"><input type="radio" name="clickTargetBehavior" value="whitelist"> Whitelist Target</label>
		</div>
		<div class="hint">Keyboard Method keeps direct-window keys for combat. All methods use the same saved click areas for party, resu, DC, and chat invites. Keypress Method sends combat keys through Windows; the game must remain focused. Click Method also uses saved click areas for combat. The picker returns to KaTools, and the cursor returns after each click.</div>
	</div>

	<div class="section" id="targetPanelAreaSection">

		<h2>Target Panel Area</h2>

		<div class="roi-picker-row">
			<div>
				<div class="roi-picker-title">Target panel area (shared)</div>
				<div id="targetROIInfo" class="party-roi-info">Select the target name and red HP bar first</div>
				<div class="hint">Used by Whitelist Target, Target Until Dead, and Emergency skills that need a target.</div>
			</div>
			<button type="button" class="party-picker-button" onclick="openTargetPicker()" title="Select the current target name and red HP bar">SET AREA</button>
		</div>
		<div id="clickWhitelistNamesSettings" class="target-name-card" hidden>
			<label for="clickWhitelistTargetNames">Whitelist Target Names</label>
			<input type="text" id="clickWhitelistTargetNames" placeholder="Example: Vasabhum;Zarku Rudhira" autocomplete="off">
			<div class="hint">Only targets whose name matches this list will activate the unchecked skill slots. Separate names with a semicolon.</div>
		</div>

		<img id="targetROIPreview" class="party-roi-preview" alt="Selected target name and HP bar preview">

	</div>

	<div class="section">

		<h2>Actions</h2>

		<div class="actions-grid">

			<div class="action-card party-action-card">

				<input
					type="checkbox"
					id="autoAccept"
					disabled>

				<div class="party-invite-controls">
					<label for="autoAccept">Auto Party Invite</label>
					<div class="party-invite-actions" role="radiogroup" aria-label="Party invite action">
						<label><input type="radio" name="partyInviteAction" value="accept" id="partyInviteAccept" checked> Accept</label>
						<label><input type="radio" name="partyInviteAction" value="reject" id="partyInviteReject"> Reject</label>
					</div>
				</div>

				<button
					type="button"
					class="party-picker-button"
					onclick="openPartyPicker()"
					title="Legacy Party OCR area (unused)" hidden>

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
					title="Legacy death dialog area (unused)" hidden>

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

		<div class="roi-picker-row" id="partyInviteActionAreas">
			<div><div class="roi-picker-title">Party Invite Accept pointer (left)</div><div class="click-area-controls" id="clickAreaControlsAutoAccept"><span class="click-area-status" id="clickAreaStatusAutoAccept">Pointer not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('AutoAccept')">SET POINTER</button><button type="button" id="clickAreaResetAutoAccept" onclick="resetClickSkillArea('AutoAccept')">RESET</button></div></div></div>
			<div><div class="roi-picker-title">Party Invite Reject pointer (right)</div><div class="click-area-controls" id="clickAreaControlsAutoReject"><span class="click-area-status" id="clickAreaStatusAutoReject">Pointer not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('AutoReject')">SET POINTER</button><button type="button" id="clickAreaResetAutoReject" onclick="resetClickSkillArea('AutoReject')">RESET</button></div></div></div>
		</div>

		<div class="roi-picker-row click-global-action-area" id="clickGlobalActionAreas" hidden>
			<div><div class="roi-picker-title">Auto Resu click point</div><div class="click-area-controls" id="clickAreaControlsAutoResu"><span class="click-area-status" id="clickAreaStatusAutoResu">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('AutoResu')">SET AREA</button><button type="button" id="clickAreaResetAutoResu" onclick="resetClickSkillArea('AutoResu')">RESET</button></div></div></div>
			<div><div class="roi-picker-title">DC dialog OK click point</div><div class="click-area-controls" id="clickAreaControlsDCOk"><span class="click-area-status" id="clickAreaStatusDCOk">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('DCOk')">SET AREA</button><button type="button" id="clickAreaResetDCOk" onclick="resetClickSkillArea('DCOk')">RESET</button></div></div></div>
		</div>

		<div class="roi-picker-row">
			<div><div class="roi-picker-title">Party mode: Normal / Automatic</div><div class="click-area-controls" id="clickAreaControlsPartyAutomatic"><span class="click-area-status" id="clickAreaStatusPartyAutomatic">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('PartyAutomatic')">SET AREA</button><button type="button" id="clickAreaResetPartyAutomatic" onclick="resetClickSkillArea('PartyAutomatic')">RESET</button></div></div><div class="hint">Select the full loot-mode button, including enough width for Master Loot. KaTools clicks only when OCR reads Normal; Automatic and Master Loot are skipped.</div></div>
			<img id="partyAutomaticROIPreview" class="party-roi-preview" alt="Party Normal / Automatic scan area preview">
		</div>

		<div class="roi-picker-row click-global-action-area" id="clickPopupScanAreas" hidden>
			<div class="click-popup-scan-card">
				<div class="roi-picker-title">Party popup scan area</div>
				<div class="click-area-controls"><span class="click-area-status" id="clickPopupScanStatusParty">Loading area...</span><div class="click-area-buttons"><button type="button" class="chat-scan-area-button" onclick="openClickPopupScanPicker('party')">SET SCAN AREA</button><button type="button" onclick="resetClickPopupScanArea('party')">RESET</button></div></div>
				<img class="party-roi-preview click-popup-scan-preview" id="clickPopupScanPreviewParty" alt="Party popup scan area preview">
			</div>
			<div class="click-popup-scan-card">
				<div class="roi-picker-title">Death / Resu popup scan area</div>
				<div class="click-area-controls"><span class="click-area-status" id="clickPopupScanStatusDeath">Loading area...</span><div class="click-area-buttons"><button type="button" class="chat-scan-area-button" onclick="openClickPopupScanPicker('death')">SET SCAN AREA</button><button type="button" onclick="resetClickPopupScanArea('death')">RESET</button></div></div>
				<img class="party-roi-preview click-popup-scan-preview" id="clickPopupScanPreviewDeath" alt="Death and resurrection popup scan area preview">
			</div>
			<div class="click-popup-scan-card">
				<div class="roi-picker-title">DC popup scan area</div>
				<div class="click-area-controls"><span class="click-area-status" id="clickPopupScanStatusDc">Loading area...</span><div class="click-area-buttons"><button type="button" class="chat-scan-area-button" onclick="openClickPopupScanPicker('dc')">SET SCAN AREA</button><button type="button" onclick="resetClickPopupScanArea('dc')">RESET</button></div></div>
				<img class="party-roi-preview click-popup-scan-preview" id="clickPopupScanPreviewDc" alt="Disconnect popup scan area preview">
			</div>
		</div>

		<div class="roi-picker-row" id="chatTextClickSettings" hidden>
			<div>
				<div class="roi-picker-title"><label><input type="checkbox" id="chatTextClickEnabled"> Chat keyword party invite</label></div>
				<div class="chat-text-invite-controls" id="chatTextInviteControls">
					<div class="chat-text-invite-field">
						<label for="chatTextClickKeyword">Keyword</label>
						<input type="text" id="chatTextClickKeyword" placeholder="Add Party Please" autocomplete="off">
					</div>
					<div class="chat-text-invite-field chat-text-invite-scan">
						<label>Chat scan area</label>
						<div class="click-area-controls"><span class="click-area-status" id="chatTextROIInfo">Scan area not set</span><div class="click-area-buttons"><button type="button" class="chat-scan-area-button" onclick="openChatTextPicker()">SET SCAN AREA</button></div></div>
					</div>
					<div class="chat-text-invite-field chat-text-invite-pointer">
						<div class="roi-picker-title">Party action pointer</div>
						<div class="click-area-controls" id="clickAreaControlsChatParty"><span class="click-area-status" id="clickAreaStatusChatParty">Pointer not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('ChatParty')">SET PARTY POINTER</button><button type="button" id="clickAreaResetChatParty" onclick="resetClickSkillArea('ChatParty')">RESET</button></div></div>
					</div>
					<div class="chat-text-invite-field">
						<label for="chatTextClickYOffset">Click below text (px)</label>
						<input type="number" class="numeric-input" id="chatTextClickYOffset" min="0" max="200" step="1" value="100">
					</div>
				</div>
				<div class="hint">Available in Keyboard, Keypress, and Click Method. When the phrase is found, KaTools clicks the saved Party pointer first, then clicks below the floating text to invite that character.</div>
			</div>
		</div>

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
					<div class="click-area-controls" id="clickAreaControlsHP" hidden><span class="click-area-status" id="clickAreaStatusHP">HP area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('HP')">SET AREA</button><button type="button" id="clickAreaResetHP" onclick="resetClickSkillArea('HP')">RESET</button></div></div>
				</div>
				<div class="pot-card">
					<input type="checkbox" id="autoPotTP" disabled>
					<label for="autoPotTP">TP Pot</label>
					<input type="text" inputmode="decimal" class="numeric-input" id="autoPotTPPercent" value="0" placeholder="%">
					<select id="autoPotTPSlot"><option value="49">1</option><option value="50">2</option><option value="51">3</option><option value="52">4</option><option value="53">5</option><option value="54">6</option><option value="55">7</option><option value="56">8</option><option value="57">9</option><option value="48">0</option><option value="112">F1</option><option value="113">F2</option><option value="114">F3</option><option value="115">F4</option><option value="116">F5</option><option value="117">F6</option><option value="118">F7</option><option value="119">F8</option><option value="120">F9</option><option value="121">F10</option></select>
					<div class="click-area-controls" id="clickAreaControlsTP" hidden><span class="click-area-status" id="clickAreaStatusTP">TP area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('TP')">SET AREA</button><button type="button" id="clickAreaResetTP" onclick="resetClickSkillArea('TP')">RESET</button></div></div>
				</div>
			</div>
			<div class="hint" id="autoPotClickHint" hidden>In Click mode, HP Pot and TP Pot each use their own saved click point. Set an area for each pot you enable.</div>

		</div>

		<div class="actions-grid" id="keyboardActionsGrid">

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

			<div class="action-card" id="attackActionCard">

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

			<div class="action-card" id="pickActionCard">

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

		<h2 id="normalNumberSkillsTitle">1 - 0</h2>

		<div class="hint" id="skillInputHint">Choose keys from 1 - 0. F1 - F10 always have priority during timing clashes.</div>
		<div class="hint" id="skillLiveApplyStatus" role="status" aria-live="polite"></div>
		<div class="skills-grid">

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill1">
				<label class="skill-name">1</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay1" value="0">
				<div class="click-area-controls" id="clickAreaControls1" hidden><span class="click-area-status" id="clickAreaStatus1">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('1')">SET AREA</button><button type="button" id="clickAreaReset1" onclick="resetClickSkillArea('1')">RESET</button></div></div>
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill2">
				<label class="skill-name">2</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay2" value="0">
				<div class="click-area-controls" id="clickAreaControls2" hidden><span class="click-area-status" id="clickAreaStatus2">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('2')">SET AREA</button><button type="button" id="clickAreaReset2" onclick="resetClickSkillArea('2')">RESET</button></div></div>
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill3">
				<label class="skill-name">3</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay3" value="0">
				<div class="click-area-controls" id="clickAreaControls3" hidden><span class="click-area-status" id="clickAreaStatus3">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('3')">SET AREA</button><button type="button" id="clickAreaReset3" onclick="resetClickSkillArea('3')">RESET</button></div></div>
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill4">
				<label class="skill-name">4</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay4" value="0">
				<div class="click-area-controls" id="clickAreaControls4" hidden><span class="click-area-status" id="clickAreaStatus4">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('4')">SET AREA</button><button type="button" id="clickAreaReset4" onclick="resetClickSkillArea('4')">RESET</button></div></div>
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skill5">
				<label class="skill-name">5</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay5" value="0">
				<div class="click-area-controls" id="clickAreaControls5" hidden><span class="click-area-status" id="clickAreaStatus5">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('5')">SET AREA</button><button type="button" id="clickAreaReset5" onclick="resetClickSkillArea('5')">RESET</button></div></div>
			</div>

			<div class="skill-card click-only-skill" hidden><input class="skill-check" type="checkbox" id="skill6"><label class="skill-name">6</label><input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay6" value="0"><div class="click-area-controls" id="clickAreaControls6" hidden><span class="click-area-status" id="clickAreaStatus6">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('6')">SET AREA</button><button type="button" id="clickAreaReset6" onclick="resetClickSkillArea('6')">RESET</button></div></div></div>
			<div class="skill-card click-only-skill" hidden><input class="skill-check" type="checkbox" id="skill7"><label class="skill-name">7</label><input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay7" value="0"><div class="click-area-controls" id="clickAreaControls7" hidden><span class="click-area-status" id="clickAreaStatus7">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('7')">SET AREA</button><button type="button" id="clickAreaReset7" onclick="resetClickSkillArea('7')">RESET</button></div></div></div>
			<div class="skill-card click-only-skill" hidden><input class="skill-check" type="checkbox" id="skill8"><label class="skill-name">8</label><input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay8" value="0"><div class="click-area-controls" id="clickAreaControls8" hidden><span class="click-area-status" id="clickAreaStatus8">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('8')">SET AREA</button><button type="button" id="clickAreaReset8" onclick="resetClickSkillArea('8')">RESET</button></div></div></div>
			<div class="skill-card click-only-skill" hidden><input class="skill-check" type="checkbox" id="skill9"><label class="skill-name">9</label><input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay9" value="0"><div class="click-area-controls" id="clickAreaControls9" hidden><span class="click-area-status" id="clickAreaStatus9">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('9')">SET AREA</button><button type="button" id="clickAreaReset9" onclick="resetClickSkillArea('9')">RESET</button></div></div></div>
			<div class="skill-card click-only-skill" hidden><input class="skill-check" type="checkbox" id="skill0"><label class="skill-name">0</label><input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delay0" value="0"><div class="click-area-controls" id="clickAreaControls0" hidden><span class="click-area-status" id="clickAreaStatus0">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('0')">SET AREA</button><button type="button" id="clickAreaReset0" onclick="resetClickSkillArea('0')">RESET</button></div></div></div>

		</div>

	</div>

	<div class="section" id="normalFunctionSkillsSection">

		<h2 id="normalFunctionSkillsTitle">F1 - F10</h2>

		<div class="hint">F1 - F10 pre-fire ~300ms early and always win timing clashes against 1 - 0.</div>

		<div class="skills-grid">

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF1">
				<label class="skill-name">F1</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF1" value="0">
				<div class="click-area-controls" id="clickAreaControlsF1" hidden><span class="click-area-status" id="clickAreaStatusF1">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('F1')">SET AREA</button><button type="button" id="clickAreaResetF1" onclick="resetClickSkillArea('F1')">RESET</button></div></div>
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF2">
				<label class="skill-name">F2</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF2" value="0">
				<div class="click-area-controls" id="clickAreaControlsF2" hidden><span class="click-area-status" id="clickAreaStatusF2">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('F2')">SET AREA</button><button type="button" id="clickAreaResetF2" onclick="resetClickSkillArea('F2')">RESET</button></div></div>
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF3">
				<label class="skill-name">F3</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF3" value="0">
				<div class="click-area-controls" id="clickAreaControlsF3" hidden><span class="click-area-status" id="clickAreaStatusF3">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('F3')">SET AREA</button><button type="button" id="clickAreaResetF3" onclick="resetClickSkillArea('F3')">RESET</button></div></div>
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF4">
				<label class="skill-name">F4</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF4" value="0">
				<div class="click-area-controls" id="clickAreaControlsF4" hidden><span class="click-area-status" id="clickAreaStatusF4">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('F4')">SET AREA</button><button type="button" id="clickAreaResetF4" onclick="resetClickSkillArea('F4')">RESET</button></div></div>
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF5">
				<label class="skill-name">F5</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF5" value="0">
				<div class="click-area-controls" id="clickAreaControlsF5" hidden><span class="click-area-status" id="clickAreaStatusF5">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('F5')">SET AREA</button><button type="button" id="clickAreaResetF5" onclick="resetClickSkillArea('F5')">RESET</button></div></div>
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF6">
				<label class="skill-name">F6</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF6" value="0">
				<div class="click-area-controls" id="clickAreaControlsF6" hidden><span class="click-area-status" id="clickAreaStatusF6">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('F6')">SET AREA</button><button type="button" id="clickAreaResetF6" onclick="resetClickSkillArea('F6')">RESET</button></div></div>
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF7">
				<label class="skill-name">F7</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF7" value="0">
				<div class="click-area-controls" id="clickAreaControlsF7" hidden><span class="click-area-status" id="clickAreaStatusF7">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('F7')">SET AREA</button><button type="button" id="clickAreaResetF7" onclick="resetClickSkillArea('F7')">RESET</button></div></div>
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF8">
				<label class="skill-name">F8</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF8" value="0">
				<div class="click-area-controls" id="clickAreaControlsF8" hidden><span class="click-area-status" id="clickAreaStatusF8">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('F8')">SET AREA</button><button type="button" id="clickAreaResetF8" onclick="resetClickSkillArea('F8')">RESET</button></div></div>
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF9">
				<label class="skill-name">F9</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF9" value="0">
				<div class="click-area-controls" id="clickAreaControlsF9" hidden><span class="click-area-status" id="clickAreaStatusF9">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('F9')">SET AREA</button><button type="button" id="clickAreaResetF9" onclick="resetClickSkillArea('F9')">RESET</button></div></div>
			</div>

			<div class="skill-card">
				<input class="skill-check" type="checkbox" id="skillF10">
				<label class="skill-name">F10</label>
				<input class="skill-delay numeric-input" type="text" inputmode="decimal" id="delayF10" value="0">
				<div class="click-area-controls" id="clickAreaControlsF10" hidden><span class="click-area-status" id="clickAreaStatusF10">Area not set</span><div class="click-area-buttons"><button type="button" onclick="openClickSkillPicker('F10')">SET AREA</button><button type="button" id="clickAreaResetF10" onclick="resetClickSkillArea('F10')">RESET</button></div></div>
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

	<div id="autoLoginTab" class="tab-panel">
		<div class="section" id="gameLaunchSection">
			<h2>Auto Login</h2>
			<label class="auto-login-enable" id="autoLoginEnabledLabel" for="autoLoginEnabled">
				<input type="checkbox" id="autoLoginEnabled" onchange="setAutoLoginEnabled()">
				Enable Auto Login
			</label>
			<label class="auto-login-enable" for="autoLoginRunAutoHunt">
				<input type="checkbox" id="autoLoginRunAutoHunt" onchange="setAutoLoginRunAutoHunt()">
				Run Auto Hunt after login
			</label>
			<label class="auto-login-enable" for="autoLoginAutoHuntKey">
				Auto Hunt key
				<input type="text" id="autoLoginAutoHuntKey" class="auto-login-key" value="Y" maxlength="1" autocomplete="off" onchange="setAutoLoginAutoHuntKey()">
			</label>
			<div class="hint" id="autoLoginDCScanGuide" hidden>Set the <strong>DC popup scan area</strong> first: open Bot, select Keyboard, Keypress, or Click Method, then use <strong>SET SCAN AREA</strong> in the DC popup scan area card. <button type="button" class="auto-login-dc-guide-button" onclick="openAutoLoginDCScanSetup()">OPEN DC SCAN AREA</button></div>
			<div class="window-panel">
				<div id="gameLaunchFolder" class="window-info game-launch-folder">No game folder selected.</div>
				<div class="game-launch-actions">
					<button type="button" class="game-launch-button" id="chooseGameFolderButton" onclick="chooseGameFolder()">CHOOSE FOLDER</button>
					<select id="gameExecutableSelect" class="window-select" onchange="selectGameExecutable()" disabled>
						<option value="">Choose a game folder first</option>
					</select>
					<button type="button" class="game-launch-button" id="launchGameButton" onclick="launchGame()" disabled>RUN GAME</button>
				</div>
				<div id="gameLaunchInfo" class="window-info">Choose the folder containing the game's .exe file.</div>
			</div>
			<div class="roi-picker-row" id="autoLoginPointerAreas">
				<div><div class="roi-picker-title">Select Character pointer</div><div class="click-area-controls" id="clickAreaControlsAutoLoginCharacter"><span class="click-area-status" id="clickAreaStatusAutoLoginCharacter">Pointer not set</span><div class="click-area-buttons"><button type="button" id="autoLoginCharacterSet" onclick="openClickSkillPicker('AutoLoginCharacter')">SET POINTER</button><button type="button" id="clickAreaResetAutoLoginCharacter" onclick="resetClickSkillArea('AutoLoginCharacter')">RESET</button></div></div></div>
				<div><div class="roi-picker-title">Login OK pointer</div><div class="click-area-controls" id="clickAreaControlsAutoLoginOK"><span class="click-area-status" id="clickAreaStatusAutoLoginOK">Pointer not set</span><div class="click-area-buttons"><button type="button" id="autoLoginOKSet" onclick="openClickSkillPicker('AutoLoginOK')">SET POINTER</button><button type="button" id="clickAreaResetAutoLoginOK" onclick="resetClickSkillArea('AutoLoginOK')">RESET</button></div></div></div>
				<div><div class="roi-picker-title">Activate Auto Hunt pointer</div><div class="click-area-controls" id="clickAreaControlsAutoLoginAutoHunt"><span class="click-area-status" id="clickAreaStatusAutoLoginAutoHunt">Pointer not set</span><div class="click-area-buttons"><button type="button" id="autoLoginAutoHuntSet" onclick="openClickSkillPicker('AutoLoginAutoHunt')">SET POINTER</button><button type="button" id="clickAreaResetAutoLoginAutoHunt" onclick="resetClickSkillArea('AutoLoginAutoHunt')">RESET</button></div></div></div>
			</div>
			<div class="hint">Run the game and select its window in the Bot tab before setting pointers. When enabled, DC recovery waits 30 seconds before opening the game, then waits 35 seconds. A slow start gets an extra 15-second window check; if needed, KaTools retries the launch up to 3 times. It then sends two virtual Enters 6 seconds apart, selects the character, waits 10 seconds, clicks Login OK twice 3 seconds apart, waits 10 seconds, sends the configured Auto Hunt key and activates Auto Hunt, sends that key again after 7 seconds, then sends Insert and 20 virtual backward scrolls after another 7 seconds.</div>
		</div>
	</div>

</div>

<nav class="scroll-nav" aria-label="Page navigation">
	<button type="button" id="scrollTopButton" class="scroll-nav-button" onclick="scrollPageTo('top')" title="Go to top" aria-label="Go to top">&#8593;</button>
	<button type="button" id="scrollBottomButton" class="scroll-nav-button" onclick="scrollPageTo('bottom')" title="Go to bottom" aria-label="Go to bottom">&#8595;</button>
</nav>


<script>

const emergencySkillOptions = [
	["49", "1"], ["50", "2"], ["51", "3"], ["52", "4"], ["53", "5"],
	["54", "6"], ["55", "7"], ["56", "8"], ["57", "9"], ["48", "0"],
	["112", "F1"], ["113", "F2"], ["114", "F3"], ["115", "F4"], ["116", "F5"],
	["117", "F6"], ["118", "F7"], ["119", "F8"], ["120", "F9"], ["121", "F10"]
];

let botRuntimeActive = false;
let autoLoginDCScanReady = false;
let autoLoginConfig = {};
// Keep the first call-to-action simple. Once the bot has run during this page
// session, the button can advertise the Alt+F12 toggle shortcut instead.
let botHasStarted = false;
let onlineAccessAllowed = false;
let targetROIReady = false;
let windowsLoading = false;

function scrollPageTo(edge) {
	const top = edge === "bottom" ? document.documentElement.scrollHeight : 0;
	window.scrollTo({ top: top, behavior: "smooth" });
}

function updateScrollNav() {
	const topButton = document.getElementById("scrollTopButton");
	const bottomButton = document.getElementById("scrollBottomButton");
	if (!topButton || !bottomButton) return;
	topButton.disabled = window.scrollY <= 4;
	bottomButton.disabled = window.scrollY + window.innerHeight >= document.documentElement.scrollHeight - 4;
}

window.addEventListener("scroll", updateScrollNav, { passive: true });
window.addEventListener("resize", updateScrollNav);
requestAnimationFrame(updateScrollNav);

function showTab(tabName) {
	for (const panel of document.querySelectorAll(".tab-panel")) {
		panel.classList.toggle("active", panel.id === tabName + "Tab");
	}
	for (const button of document.querySelectorAll(".tab-button")) {
		button.classList.toggle("active", button.dataset.tab === tabName);
	}
}

function setTheme(theme) {
	const selectedTheme = theme === "light" ? "light" : "dark";
	document.body.classList.toggle("light-theme", selectedTheme === "light");
	document.getElementById("themeSelect").value = selectedTheme;
	try {
		localStorage.setItem("katools-theme", selectedTheme);
	} catch (error) {
		// Theme still works for the current page if browser storage is unavailable.
		console.warn("Could not save theme preference", error);
	}
}

function restoreTheme() {
	let savedTheme = "dark";
	try {
		savedTheme = localStorage.getItem("katools-theme") || "dark";
	} catch (error) {
		console.warn("Could not read theme preference", error);
	}
	setTheme(savedTheme);
}

function installROIControls() {
	const controls = [
		{ kind: "status", action: "openStatusPicker()" },
		{ kind: "target", action: "openTargetPicker()" },
		{ kind: "chat-text", action: "openChatTextPicker()" }
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
	const labels = { party: "Party OCR", death: "death dialog", status: "HP / TP status", target: "target panel", "chat-text": "chat text scan" };
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
		const loaders = { party: loadPartyROI, death: loadDeathROI, status: loadStatusROI, target: loadTargetROI, "chat-text": loadChatTextROI };
		await loaders[kind]();
		if (kind === "chat-text") document.getElementById("chatTextClickEnabled").checked = false;
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
			'<div class="click-area-controls" id="clickAreaControlsEmergency' + slot + '" hidden>' +
				'<span class="click-area-status" id="clickAreaStatusEmergency' + slot + '">Area not set</span>' +
				'<div class="click-area-buttons">' +
					'<button type="button" onclick="openClickSkillPicker(\'Emergency' + slot + '\')">SET AREA</button>' +
					'<button type="button" id="clickAreaResetEmergency' + slot + '" onclick="resetClickSkillArea(\'Emergency' + slot + '\')">RESET</button>' +
				'</div>' +
			'</div>' +
			'</div>';
	}).join("");
	syncEmergencySkillSlots();
}

const supportSkillSlots = [
	["1", 0x31], ["2", 0x32], ["3", 0x33], ["4", 0x34], ["5", 0x35]
];

const clickSkillSlots = ["1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "HP", "TP", "AutoAccept", "AutoReject", "AutoResu", "DCOk", "ChatParty", "PartyAutomatic", "AutoLoginCharacter", "AutoLoginOK", "AutoLoginAutoHunt"];
clickSkillSlots.push("Emergency1", "Emergency2", "Emergency3", "Emergency4", "Emergency5");

function clickSkillUsesScanArea(slot) {
	return slot === "PartyAutomatic";
}

// Most click-mode selections persist one coordinate. Keep their labels honest:
// only PartyAutomatic is a real rectangle because it is also an OCR source.
function updateClickSkillPickerLabels() {
	for (const button of document.querySelectorAll('button[onclick^="openClickSkillPicker"]')) {
		const match = (button.getAttribute("onclick") || "").match(/openClickSkillPicker\('([^']+)'\)/);
		if (!match) continue;
		const isArea = clickSkillUsesScanArea(match[1]);
		button.textContent = isArea ? "SET AREA" : "SET POINTER";
		button.title = isArea ? "Select the OCR area and click centre" : "Set one click point";
	}
}

function ensureClickTargetToggles() {
	for (const slot of clickSkillSlots.slice(0, 20)) {
		const delay = document.getElementById("delay" + slot);
		if (!delay || document.getElementById("targetSearch" + slot)) continue;
		const label = document.createElement("label");
		label.className = "click-target-toggle";
		label.hidden = true;
		label.title = "Use this slot to search for a target until a whitelisted name is found.";
		const checkbox = document.createElement("input");
		checkbox.type = "checkbox";
		checkbox.id = "targetSearch" + slot;
		label.appendChild(checkbox);
		label.appendChild(document.createTextNode("Target?"));
		delay.parentElement.insertBefore(label, delay);
	}
}

function getTargetInputMethod() {
	const selected = document.querySelector('input[name="targetInputMethod"]:checked');
	return selected ? selected.value : "keyboard";
}

function usesPopupClickMethod() {
	const method = getTargetInputMethod();
	// Combat input remains method-specific, but every method shares the
	// pointer-based popup flow for party, resurrection, DC, and chat invites.
	return method === "keyboard" || method === "keypress" || method === "click";
}

function isClickWhitelistMode() {
	const selected = document.querySelector('input[name="clickTargetBehavior"]:checked');
	return !!selected && selected.value === "whitelist";
}

async function loadClickSkillAreas() {
	try {
		updateClickSkillPickerLabels();
		const response = await fetch("/api/click-skill/areas", { cache: "no-store" });
		const result = await response.json();
		const areas = result.areas || {};
		for (const slot of clickSkillSlots) {
			const autoLoginSlot = slot.startsWith("AutoLogin");
			const profilePointers = autoLoginConfig.pointers || {};
			const area = autoLoginSlot ? profilePointers[slot] : areas[slot];
		const status = document.getElementById("clickAreaStatus" + slot);
			const reset = document.getElementById("clickAreaReset" + slot);
			if (!status || !reset) continue;
			const selected = !!(area && area.selected);
			const label = clickSkillUsesScanArea(slot) ? "Area" : "Pointer";
			status.textContent = selected ? label + " set" : label + " not set";
			reset.disabled = !selected || !hasSelectedTargetWindow() || (autoLoginSlot && !document.getElementById("autoLoginEnabled").checked);
		}
		syncAutoLoginControls();
	} catch (error) {
		console.error("Failed to load click skill areas:", error);
	}
}

function isAutoLoginPointerSlot(slot) {
	return slot === "AutoLoginCharacter" || slot === "AutoLoginOK" || slot === "AutoLoginAutoHunt";
}

async function saveAutoLoginPointer(slot, pointer) {
	const response = await fetch("/api/game-launch/pointer", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ slot: slot, pointer: pointer })
	});
	const result = await response.json();
	if (!result.success) throw new Error(result.message || "Failed to save the Auto Login pointer.");
	autoLoginConfig = Object.assign({}, result.config || {});
}

async function openClickSkillPicker(slot) {
	const hwnd = document.getElementById("windowSelect").value;
	if (!hwnd) {
		alert("Select a target window first.");
		return;
	}
	const beforeResponse = await fetch("/api/click-skill/areas", { cache: "no-store" });
	const beforeResult = await beforeResponse.json();
	const beforeArea = JSON.stringify((beforeResult.areas || {})[slot] || null);
	const response = await fetch("/api/click-skill/picker", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ hwnd: hwnd, slot: slot })
	});
	const result = await response.json();
	if (!result.success) {
		alert(result.message || "Failed to open the skill area picker.");
		return;
	}
	let refreshes = 0;
	const timer = setInterval(async function() {
		await loadClickSkillAreas();
		if (slot === "PartyAutomatic") await loadPartyAutomaticPreview();
		const areaResponse = await fetch("/api/click-skill/areas", { cache: "no-store" });
		const areaResult = await areaResponse.json();
		const area = (areaResult.areas || {})[slot];
		if ((area && area.selected && JSON.stringify(area) !== beforeArea) || ++refreshes >= 30) {
			clearInterval(timer);
			if (area && area.selected && JSON.stringify(area) !== beforeArea) {
				try {
					if (isAutoLoginPointerSlot(slot)) await saveAutoLoginPointer(slot, area);
					scheduleLiveConfig();
					await loadClickSkillAreas();
				} catch (error) {
					console.error("Failed to save Auto Login pointer:", error);
					alert(error.message || "Failed to save the Auto Login pointer.");
				}
			}
		}
	}, 1000);
}

async function resetClickSkillArea(slot) {
	const response = await fetch("/api/click-skill/reset", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ slot: slot })
	});
	const result = await response.json();
	if (!result.success) {
		alert(result.message || "Failed to reset the skill area.");
		return;
	}
	if (isAutoLoginPointerSlot(slot)) {
		const pointerResponse = await fetch("/api/game-launch/pointer/reset", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ slot: slot })
		});
		const pointerResult = await pointerResponse.json();
		if (!pointerResult.success) {
			alert(pointerResult.message || "Failed to reset the Auto Login pointer.");
			return;
		}
		autoLoginConfig = Object.assign({}, pointerResult.config || {});
	}
	await loadClickSkillAreas();
	if (slot === "PartyAutomatic") await loadPartyAutomaticPreview();
	scheduleLiveConfig();
}

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
	const clickMode = getTargetInputMethod() === "click";
	const clickWhitelistMode = clickMode && isClickWhitelistMode();
	const panic = document.getElementById("assistPanicTarget");
	if (panic) {
		const panicLabel = panic.closest(".emergency-panic-row");
		if (panicLabel) panicLabel.hidden = clickMode;
		// A saved config can contain this option while the target ROI is not
		// available yet. Keep an already enabled option clickable so the user can
		// turn it off; only lock options that would turn it on.
		panic.disabled = clickMode || (!targetEnabled && !panic.checked);
	}
	for (let slot = 1; slot <= 5; slot++) {
		const checkbox = document.getElementById("emergency" + slot);
		const select = document.getElementById("emergencySlot" + slot);
		const needsTarget = document.getElementById("emergencyNeedTarget" + slot);
		if (!checkbox || !select || !needsTarget) continue;
		const targetLabel = needsTarget.closest(".emergency-target-label");
		const clickControls = document.getElementById("clickAreaControlsEmergency" + slot);
		checkbox.disabled = !enabled;
		select.disabled = !enabled || !checkbox.checked;
		if (targetLabel) targetLabel.hidden = clickMode && !clickWhitelistMode;
		if (clickControls) clickControls.hidden = !clickMode;
		// Same rule for loaded Need Target slots: they must remain uncheckable
		// even before the Target panel has been selected again.
		needsTarget.disabled = !targetEnabled && !needsTarget.checked;
	}
	const hint = document.getElementById("emergencyTargetHint");
	if (hint) {
		hint.innerText = clickMode
			? (clickWhitelistMode
				? "In Whitelist Target mode, Need Target Emergency skills wait for a whitelisted target. Set a click area for each Emergency slot you enable."
				: "Without Name mode Emergency skills do not wait for a target. Set a click area for each Emergency slot you enable.")
			: (targetROIReady
				? "Need Target skills use the selected Target panel. In normal assist mode KaTools never sends E."
				: "Set Target panel area first to enable Need Target or Assist Panic Target.");
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

		{ name: "6", vk: 0x36, enabled: document.getElementById("skill6").checked, delay: Number(document.getElementById("delay6").value) },
		{ name: "7", vk: 0x37, enabled: document.getElementById("skill7").checked, delay: Number(document.getElementById("delay7").value) },
		{ name: "8", vk: 0x38, enabled: document.getElementById("skill8").checked, delay: Number(document.getElementById("delay8").value) },
		{ name: "9", vk: 0x39, enabled: document.getElementById("skill9").checked, delay: Number(document.getElementById("delay9").value) },
		{ name: "0", vk: 0x30, enabled: document.getElementById("skill0").checked, delay: Number(document.getElementById("delay0").value) },

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

	].map(function(skill) {
		const targetSearch = document.getElementById("targetSearch" + skill.name);
		skill.targetSearch = !!(targetSearch && targetSearch.checked);
		return skill;
	}).filter(function(skill) {
		const numberSlot = skill.vk >= 0x30 && skill.vk <= 0x39;
		const functionSlot = skill.vk >= 0x70 && skill.vk <= 0x79;
		return numberSlot || functionSlot;
	});

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

		autoLogin: autoLoginProfileConfig(),

		autoAcceptEnabled:
			document.getElementById("autoAccept").checked,

		partyInviteAction:
			(document.querySelector('input[name="partyInviteAction"]:checked') || {}).value || "accept",

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

		targetWithClick:
			getTargetInputMethod() === "click",

		inputMethod:
			getTargetInputMethod(),

		clickWhitelistMode:
			getTargetInputMethod() === "click" && isClickWhitelistMode(),

		clickWhitelistTargetNames:
			document.getElementById("clickWhitelistTargetNames").value,
		chatTextClickEnabled: document.getElementById("chatTextClickEnabled").checked,
		chatTextClickKeyword: document.getElementById("chatTextClickKeyword").value,
		chatTextClickYOffset: Number(document.getElementById("chatTextClickYOffset").value),

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
				index: slot,
				enabled: document.getElementById("autoPotHP").checked && document.getElementById("emergency" + slot).checked,
				vk: Number(document.getElementById("emergencySlot" + slot).value),
				needsTarget: (getTargetInputMethod() !== "click" || isClickWhitelistMode()) && document.getElementById("emergencyNeedTarget" + slot).checked
			};
		}),

		assistPanicTarget: document.getElementById("assistPanicTarget").checked,

		skills: skills,

		supportSkills: supportSkills
	};
}

// Auto Login is shared for the game installation, except for the character
// selection point. Profiles therefore carry only that character-specific
// pointer and never overwrite global launcher settings when loaded.
function autoLoginProfileConfig() {
	const pointers = (autoLoginConfig && autoLoginConfig.pointers) || {};
	const characterPointer = pointers.AutoLoginCharacter;
	return characterPointer && characterPointer.selected ? {
		pointers: { AutoLoginCharacter: characterPointer }
	} : {};
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
	// The button already tracks runtime state via updateStatus(). Do not put a
	// status request in front of the emergency Stop request: a slow status read
	// must never prevent /api/stop from being sent.
	if (botRuntimeActive) {

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
	botHasStarted = true;
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
let clickSkillApplyRevision = 0;
let clickSkillApplyRunning = false;

// Auto Login starts the bot again on a newly-created game HWND. Keep the
// dashboard selection aligned with that runtime target so later live-setting
// changes never point back at the disconnected instance.
function syncRuntimeWindowSelection(hwnd) {
	if (!hwnd || savedWindowHWND === hwnd) {
		return;
	}
	const select = document.getElementById("windowSelect");
	savedWindowHWND = hwnd;
	if (select) {
		select.value = "";
	}
	if (!windowsLoading) {
		loadWindows();
	}
}

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

async function applyLiveClickSkills() {
	clickSkillApplyRevision++;
	if (!botRuntimeActive || getTargetInputMethod() !== "click" || clickSkillApplyRunning) return;
	clickSkillApplyRunning = true;
	const status = document.getElementById("skillLiveApplyStatus");
	let appliedRevision = 0;
	try {
		while (appliedRevision !== clickSkillApplyRevision) {
			const revision = clickSkillApplyRevision;
			const config = getConfig();
			const invalidSkill = config.skills.find(skill => skill.enabled && (!Number.isFinite(skill.delay) || skill.delay <= 0));
			if (invalidSkill) {
				status.textContent = "Skill " + invalidSkill.name + " needs an interval greater than zero.";
				status.classList.add("error-text");
				appliedRevision = revision;
				continue;
			}
			status.textContent = "Applying Click skill changes...";
			status.classList.remove("error-text");
			const response = await fetch("/api/config/click-skills", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify(config)
			});
			const result = await response.json();
			if (!result.success) {
				status.textContent = result.message || "Click skill changes could not be applied.";
				status.classList.add("error-text");
			} else {
				status.textContent = "Click skill changes applied live.";
				status.classList.remove("error-text");
			}
			appliedRevision = revision;
		}
	} catch (error) {
		console.error("Failed to apply Click skill changes:", error);
		status.textContent = "Could not reach KaTools to apply Click skill changes.";
		status.classList.add("error-text");
	} finally {
		clickSkillApplyRunning = false;
		if (appliedRevision !== clickSkillApplyRevision) applyLiveClickSkills();
	}
}

function applySavedConfig(config) {
	if (!config) {
		return;
	}
	ensureClickTargetToggles();

	savedWindowHWND = config.hwnd || "";
	document.getElementById("autoAccept").checked = !!config.autoAcceptEnabled;
	const inviteAction = config.partyInviteAction === "reject" ? "reject" : "accept";
	const inviteActionRadio = document.querySelector('input[name="partyInviteAction"][value="' + inviteAction + '"]');
	if (inviteActionRadio) inviteActionRadio.checked = true;
	// Saved config is applied before the selected window and Death ROI finish
	// loading. Do not discard these values merely because the controls are
	// temporarily disabled; the ROI availability pass below will clear them
	// only when the required area is genuinely missing.
	document.getElementById("autoPauseDeath").checked = !!config.autoPauseDeathEnabled;
	document.getElementById("autoResurrect").checked =
		!!config.autoPauseDeathEnabled && !!config.autoResurrectEnabled;
	const inputMethod = config.inputMethod || (config.targetWithClick ? "click" : "keyboard");
	const inputMethodRadio = document.querySelector('input[name="targetInputMethod"][value="' + inputMethod + '"]');
	if (inputMethodRadio) inputMethodRadio.checked = true;
	const clickTargetBehavior = document.querySelector('input[name="clickTargetBehavior"][value="' + (config.clickWhitelistMode ? "whitelist" : "withoutName") + '"]');
	if (clickTargetBehavior) clickTargetBehavior.checked = true;
	document.getElementById("clickWhitelistTargetNames").value = config.clickWhitelistTargetNames || "";
	document.getElementById("chatTextClickEnabled").checked = !!config.chatTextClickEnabled;
	document.getElementById("chatTextClickKeyword").value = config.chatTextClickKeyword || "";
	document.getElementById("chatTextClickYOffset").value = config.chatTextClickYOffset || 100;
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
		const targetSearch = document.getElementById("targetSearch" + skill.name);
		if (checkbox) checkbox.checked = !!skill.enabled;
		if (delay) delay.value = skill.delay || "";
		if (targetSearch) targetSearch.checked = !!skill.targetSearch;
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
			await applyAutoLoginProfile(result.config.autoLogin || {});
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

async function applyAutoLoginProfile(config) {
	const response = await fetch("/api/game-launch/config", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ config: config })
	});
	const result = await response.json();
	if (!result.success) {
		throw new Error(result.message || "Failed to load Auto Login settings.");
	}
	await loadGameLauncher();
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

		const access = result.access || {};
		const accessWasAllowed = onlineAccessAllowed;
		onlineAccessAllowed = !!access.allowed;
		if (accessWasAllowed !== onlineAccessAllowed) {
			updateWindowDependentControls();
			if (onlineAccessAllowed) loadGameLauncher();
		}
		if (!onlineAccessAllowed) {
			botRuntimeActive = false;
			status.className = "status paused";
			status.innerHTML = '<span class="status-dot"></span><span>ONLINE ACCESS REQUIRED</span>';
			button.className = "bot-button start";
			button.innerText = "ACCESS LOCKED";
			button.disabled = true;
			info.innerText = access.message || "KaTools could not verify online access.";
			const statusRead = document.getElementById("statusReadInfo");
			if (statusRead) statusRead.innerText = "";
			return;
		}

		botRuntimeActive = !!(result.running || result.starting);
		if (botRuntimeActive) {
			botHasStarted = true;
			syncRuntimeWindowSelection(result.hwnd);
		}

		if (result.starting) {
			status.className = "status starting";
			status.innerHTML = '<span class="status-dot"></span><span>● STARTING...</span>';
			button.className = "bot-button stop";
			button.innerText = "Alt+F12 to Stop";
		} else if (result.running) {

			if (result.paused) {
				status.className = "status paused";
				status.innerHTML = '<span class="status-dot"></span><span>&#9679; AUTO PAUSED &mdash; DEATH DETECTED &mdash; Alt+F12 to Stop</span>';
			} else {
				status.className = "status running";
				status.innerHTML = '<span class="status-dot"></span><span>● BOT RUNNING — Alt+F12 to Stop</span>';
			}

			button.className = "bot-button stop";
			button.innerText = "Alt+F12 to Stop";

		} else {

			status.className = "status stopped";
			status.innerHTML = '<span class="status-dot"></span><span>● BOT STOPPED</span>';

			button.className = "bot-button start";
			button.innerText = botHasStarted ? "Alt+F12 to Start" : "START BOT";

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
	return onlineAccessAllowed && !!document.getElementById("windowSelect").value;
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

	// A target window is required for bot actions, but never for navigation.
	// In particular, a manual game launch starts with no selectable game HWND;
	// keeping the tabs and page navigation live lets the user go to Bot and
	// refresh/select the new window instead of having to restart KaTools.
	for (const button of document.querySelectorAll("button:not(.refresh-button):not(.game-launch-button):not(.tab-button):not(.auto-login-dc-guide-button):not(#scrollTopButton):not(#scrollBottomButton)")) {
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
		for (const id of ["autoAccept", "partyInviteAccept", "partyInviteReject", "autoPauseDeath", "autoResurrect", "autoPotHP", "autoPotTP"]) {
			document.getElementById(id).disabled = true;
		}
		for (const id of ["autoPotHPPercent", "autoPotHPSlot", "autoPotTPPercent", "autoPotTPSlot"]) {
			document.getElementById(id).disabled = true;
		}
		syncAutoLoginControls();
		return;
	}

	// ROI-dependent controls need both a selected window and their own ROI.
	loadTargetROI();
	loadClickSkillAreas();
	loadClickPopupScanAreas();
	loadStatusROI();
	syncTargetMode();
	syncEmergencySkillSlots();
	syncAutoLoginControls();
}

function renderGameLauncher(config, executables) {
	autoLoginConfig = Object.assign({}, config || {});
	const folder = (config && config.folder) || "";
	const selectedExe = (config && config.exe) || "";
	const select = document.getElementById("gameExecutableSelect");
	const launchButton = document.getElementById("launchGameButton");
	const folderLabel = document.getElementById("gameLaunchFolder");
	const info = document.getElementById("gameLaunchInfo");
	document.getElementById("autoLoginEnabled").checked = !!(config && config.enabled && autoLoginDCScanReady && onlineAccessAllowed);
	document.getElementById("autoLoginRunAutoHunt").checked = !config || config.runAutoHunt !== false;
	document.getElementById("autoLoginAutoHuntKey").value = (config && config.autoHuntKey) || "Y";
	folderLabel.textContent = folder ? "Game folder: " + folder : "No game folder selected.";
	select.innerHTML = "";
	const placeholder = document.createElement("option");
	placeholder.value = "";
	placeholder.textContent = folder ? "Select the game executable..." : "Choose a game folder first";
	select.appendChild(placeholder);
	for (const executable of executables || []) {
		const option = document.createElement("option");
		option.value = executable;
		option.textContent = executable;
		select.appendChild(option);
	}
	select.value = selectedExe;
	if (select.value !== selectedExe) select.value = "";
	select.disabled = !folder || !(executables || []).length || gameLaunchBusy;
	launchButton.disabled = !selectedExe || gameLaunchBusy;
	if (!folder) info.textContent = "Choose the folder containing the game's .exe file.";
	else if (!(executables || []).length) info.textContent = "No .exe file was found directly in this folder.";
	else if (!selectedExe) info.textContent = "Select the executable used to open the game.";
	syncAutoLoginControls();
}

function syncAutoLoginControls() {
	const checkbox = document.getElementById("autoLoginEnabled");
	const enabledLabel = document.getElementById("autoLoginEnabledLabel");
	const guide = document.getElementById("autoLoginDCScanGuide");
	const dcScanMessage = "Set the DC popup scan area first: Bot tab → Keyboard, Keypress, or Click Method → DC popup scan area → SET SCAN AREA.";
	const accessMessage = "Online access is required before Auto Login can be enabled.";
	const canUseAutoLogin = onlineAccessAllowed && autoLoginDCScanReady;
	if (!onlineAccessAllowed) checkbox.checked = false;
	checkbox.disabled = !canUseAutoLogin;
	checkbox.title = canUseAutoLogin ? "" : (onlineAccessAllowed ? dcScanMessage : accessMessage);
	enabledLabel.title = canUseAutoLogin ? "" : (onlineAccessAllowed ? dcScanMessage : accessMessage);
	guide.hidden = !onlineAccessAllowed || autoLoginDCScanReady;
	const enabled = checkbox.checked && canUseAutoLogin;
	document.getElementById("autoLoginRunAutoHunt").disabled = !enabled;
	document.getElementById("autoLoginAutoHuntKey").disabled = !enabled;
	const folder = document.getElementById("gameLaunchFolder").textContent !== "No game folder selected.";
	const select = document.getElementById("gameExecutableSelect");
	const selectedExe = select.value;
	document.getElementById("chooseGameFolderButton").disabled = !enabled || gameLaunchBusy;
	select.disabled = !enabled || !folder || select.options.length <= 1 || gameLaunchBusy;
	document.getElementById("launchGameButton").disabled = !enabled || !selectedExe || gameLaunchBusy;
	const windowReady = hasSelectedTargetWindow();
	for (const id of ["autoLoginCharacterSet", "autoLoginOKSet", "autoLoginAutoHuntSet"]) {
		document.getElementById(id).disabled = !enabled || !windowReady;
	}
	const autoLoginResetSlots = {
		clickAreaResetAutoLoginCharacter: "AutoLoginCharacter",
		clickAreaResetAutoLoginOK: "AutoLoginOK",
		clickAreaResetAutoLoginAutoHunt: "AutoLoginAutoHunt"
	};
	for (const id of Object.keys(autoLoginResetSlots)) {
		const button = document.getElementById(id);
		const slot = autoLoginResetSlots[id];
		const status = document.getElementById("clickAreaStatus" + slot);
		button.disabled = !enabled || !windowReady || !status || status.textContent !== "Pointer set";
	}
}

function openAutoLoginDCScanSetup() {
	showTab("main");
	const section = document.getElementById("clickPopupScanAreas");
	if (section) section.scrollIntoView({ behavior: "smooth", block: "center" });
}

async function setAutoLoginEnabled() {
	const enabled = document.getElementById("autoLoginEnabled").checked;
	syncAutoLoginControls();
	try {
		const response = await fetch("/api/game-launch/enabled", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ enabled: enabled }) });
		const result = await response.json();
		if (!result.success) throw new Error(result.message || "Failed to update Auto Login.");
	} catch (error) {
		console.error("Failed to update Auto Login:", error);
		alert(error.message || "Failed to update Auto Login.");
	} finally {
		await loadGameLauncher();
	}
}

async function setAutoLoginRunAutoHunt() {
	const runAutoHunt = document.getElementById("autoLoginRunAutoHunt").checked;
	try {
		const response = await fetch("/api/game-launch/auto-hunt", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ runAutoHunt: runAutoHunt }) });
		const result = await response.json();
		if (!result.success) throw new Error(result.message || "Failed to update Run Auto Hunt.");
	} catch (error) {
		console.error("Failed to update Run Auto Hunt:", error);
		alert(error.message || "Failed to update Run Auto Hunt.");
	} finally {
		await loadGameLauncher();
	}
}

async function setAutoLoginAutoHuntKey() {
	const input = document.getElementById("autoLoginAutoHuntKey");
	const autoHuntKey = input.value.trim().toUpperCase();
	input.value = autoHuntKey;
	try {
		const response = await fetch("/api/game-launch/auto-hunt-key", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ autoHuntKey: autoHuntKey }) });
		const result = await response.json();
		if (!result.success) throw new Error(result.message || "Auto Hunt key must be one letter from A to Z.");
	} catch (error) {
		console.error("Failed to update Auto Hunt key:", error);
		alert(error.message || "Failed to update Auto Hunt key.");
	} finally {
		await loadGameLauncher();
	}
}

let gameLaunchBusy = false;

async function loadGameLauncher() {
	try {
		const response = await fetch("/api/game-launch", { cache: "no-store" });
		const result = await response.json();
		if (!result.success) throw new Error(result.message || "Failed to load game launcher settings.");
		autoLoginDCScanReady = !!result.dcScanReady;
		renderGameLauncher(result.config || {}, result.executables || []);
	} catch (error) {
		console.error("Failed to load game launcher settings:", error);
		document.getElementById("gameLaunchInfo").textContent = error.message || "Failed to load game launcher settings.";
	}
}

async function chooseGameFolder() {
	if (gameLaunchBusy) return;
	gameLaunchBusy = true;
	document.getElementById("chooseGameFolderButton").disabled = true;
	document.getElementById("gameLaunchInfo").textContent = "Waiting for a game folder...";
	try {
		const response = await fetch("/api/game-launch/folder", { method: "POST" });
		const result = await response.json();
		if (!result.success) throw new Error(result.message || "Failed to select game folder.");
		if (result.cancelled) {
			document.getElementById("gameLaunchInfo").textContent = "Game folder selection cancelled.";
			return;
		}
		renderGameLauncher(result.config || {}, result.executables || []);
	} catch (error) {
		console.error("Failed to select game folder:", error);
		alert(error.message || "Failed to select game folder.");
	} finally {
		gameLaunchBusy = false;
		document.getElementById("chooseGameFolderButton").disabled = false;
		await loadGameLauncher();
	}
}

async function selectGameExecutable() {
	const executable = document.getElementById("gameExecutableSelect").value;
	if (!executable || gameLaunchBusy) return;
	gameLaunchBusy = true;
	try {
		const response = await fetch("/api/game-launch/executable", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ exe: executable }) });
		const result = await response.json();
		if (!result.success) throw new Error(result.message || "Failed to select game executable.");
		await loadGameLauncher();
	} catch (error) {
		console.error("Failed to select game executable:", error);
		alert(error.message || "Failed to select game executable.");
	} finally {
		gameLaunchBusy = false;
		await loadGameLauncher();
	}
}

async function launchGame() {
	if (gameLaunchBusy) return;
	gameLaunchBusy = true;
	document.getElementById("launchGameButton").disabled = true;
	document.getElementById("gameLaunchInfo").textContent = "Starting selected game...";
	try {
		const response = await fetch("/api/game-launch/run", { method: "POST" });
		const result = await response.json();
		if (!result.success) throw new Error(result.message || "Failed to start the selected game executable.");
		document.getElementById("gameLaunchInfo").textContent = "Started " + result.config.exe + ". Once its window appears, select it below.";
		setTimeout(loadWindows, 1200);
	} catch (error) {
		console.error("Failed to start game:", error);
		alert(error.message || "Failed to start the selected game executable.");
	} finally {
		gameLaunchBusy = false;
		await loadGameLauncher();
	}
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

			option.textContent = compactWindowOptionTitle(item.title);
			option.title = item.title;

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
	if (usesPopupClickMethod()) return;
	const checkbox = document.getElementById("autoAccept");
	const label = document.querySelector('label[for="autoAccept"]');
	const accept = document.getElementById("partyInviteAccept");
	const reject = document.getElementById("partyInviteReject");

	checkbox.disabled = !selected || !hasSelectedTargetWindow();
	accept.disabled = checkbox.disabled;
	reject.disabled = checkbox.disabled;
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
	if (usesPopupClickMethod()) return;
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

// Native WebView select popups size themselves from their longest option.
// Browser tabs can have a full URL as their title, which used to stretch this
// game-window list across the whole desktop. Keep the candidate selectable,
// but keep its display label compact.
function compactWindowOptionTitle(title) {
	const compact = String(title || "").replace(/\s+/g, " ").trim();
	const maxLength = 72;
	return compact.length > maxLength ? compact.slice(0, maxLength - 1) + "…" : compact;
}

async function loadClickPopupScanAreas() {
	const hwnd = document.getElementById("windowSelect").value;
	if (!hwnd) return;
	for (const kind of ["party", "death", "dc"]) {
		try {
			const response = await fetch("/api/click-popup-scan?kind=" + encodeURIComponent(kind) + "&hwnd=" + encodeURIComponent(hwnd));
			const result = await response.json();
			const suffix = kind[0].toUpperCase() + kind.slice(1);
			const status = document.getElementById("clickPopupScanStatus" + suffix);
			const preview = document.getElementById("clickPopupScanPreview" + suffix);
			if (!result.success || !result.selected) {
				if (kind === "dc") {
					autoLoginDCScanReady = false;
					syncAutoLoginControls();
				}
				status.textContent = "Scan area not set";
				if (preview.dataset.objectUrl) URL.revokeObjectURL(preview.dataset.objectUrl);
				delete preview.dataset.objectUrl;
				preview.removeAttribute("src");
				preview.style.display = "none";
				if (usesPopupClickMethod() && kind === "party") updateAutoAcceptClickAvailability(false);
				if (usesPopupClickMethod() && kind === "death") updateAutoPauseDeathClickAvailability(false);
				continue;
			}
			if (kind === "dc") {
				autoLoginDCScanReady = !!result.custom;
				syncAutoLoginControls();
			}
			status.textContent = "Scan area: X=" + result.x + " Y=" + result.y + " W=" + result.w + " H=" + result.h + (result.custom ? " (custom)" : " (default)");
			try {
				const imageResponse = await fetch("/api/click-popup-scan/preview?kind=" + encodeURIComponent(kind) + "&t=" + Date.now());
				if (imageResponse.ok) {
					if (preview.dataset.objectUrl) URL.revokeObjectURL(preview.dataset.objectUrl);
					preview.dataset.objectUrl = URL.createObjectURL(await imageResponse.blob());
					preview.src = preview.dataset.objectUrl;
					preview.style.display = "block";
				} else {
					if (preview.dataset.objectUrl) URL.revokeObjectURL(preview.dataset.objectUrl);
					delete preview.dataset.objectUrl;
					preview.removeAttribute("src");
					preview.style.display = "none";
				}
			} catch (_) { preview.style.display = "none"; }
			if (usesPopupClickMethod() && kind === "party") updateAutoAcceptClickAvailability(true);
			if (usesPopupClickMethod() && kind === "death") updateAutoPauseDeathClickAvailability(true);
		} catch (error) {
			console.error("Failed to load Click popup scan area:", kind, error);
		}
	}
}

function updateAutoAcceptClickAvailability(selected) {
	const checkbox = document.getElementById("autoAccept");
	const label = document.querySelector('label[for="autoAccept"]');
	const accept = document.getElementById("partyInviteAccept");
	const reject = document.getElementById("partyInviteReject");
	checkbox.disabled = !selected || !hasSelectedTargetWindow();
	accept.disabled = checkbox.disabled;
	reject.disabled = checkbox.disabled;
	if (!selected) { checkbox.checked = false; label.title = "Select the Click Party popup scan area first."; }
	else label.title = "";
}

function updateAutoPauseDeathClickAvailability(selected) {
	const checkbox = document.getElementById("autoPauseDeath");
	const label = document.querySelector('label[for="autoPauseDeath"]');
	const resu = document.getElementById("autoResurrect");
	const resuLabel = document.querySelector('label[for="autoResurrect"]');
	checkbox.disabled = !selected || !hasSelectedTargetWindow();
	resu.disabled = !selected || !hasSelectedTargetWindow() || !checkbox.checked;
	if (!selected) { checkbox.checked = false; resu.checked = false; label.title = "Select the Click Death / Resu popup scan area first."; resuLabel.title = "Select the Click Death / Resu scan area and enable Auto Pause on Death first."; }
	else { label.title = ""; resuLabel.title = resu.disabled ? "Enable Auto Pause on Death first." : ""; }
}

async function openClickPopupScanPicker(kind) {
	const hwnd = document.getElementById("windowSelect").value;
	if (!hwnd) { alert("Select the game window first."); return; }
	const response = await fetch("/api/click-popup-scan/picker", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ hwnd: hwnd, kind: kind }) });
	const result = await response.json();
	if (!result.success) { alert(result.message || "Failed to open popup scan area picker."); return; }
	let refreshes = 0;
	const timer = setInterval(async function() { await loadClickPopupScanAreas(); if (++refreshes >= 30) clearInterval(timer); }, 1000);
}

async function resetClickPopupScanArea(kind) {
	if (!confirm("Clear this Click popup scan area? Its coordinates and preview will be removed.")) return;
	const response = await fetch("/api/click-popup-scan/reset?kind=" + encodeURIComponent(kind), { method: "POST" });
	const result = await response.json();
	if (!result.success) { alert(result.message || "Failed to reset popup scan area."); return; }
	if (usesPopupClickMethod() && kind === "party") updateAutoAcceptClickAvailability(false);
	if (usesPopupClickMethod() && kind === "death") updateAutoPauseDeathClickAvailability(false);
	await loadClickPopupScanAreas();
	scheduleLiveConfig();
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

async function loadChatTextROI() {
	try {
		const response = await fetch("/api/chat-text-roi");
		const result = await response.json();
		const info = document.getElementById("chatTextROIInfo");
		info.textContent = result.success && result.selected
			? "Scan area: X=" + result.x + " Y=" + result.y + " W=" + result.w + " H=" + result.h
			: "Scan area not set";
	} catch (error) { console.error("Failed to load chat scan area", error); }
}

async function openChatTextPicker() {
	const hwnd = document.getElementById("windowSelect").value;
	if (!hwnd) { alert("Select the game window first."); return; }
	const response = await fetch("/api/chat-text-roi/picker", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ hwnd: hwnd }) });
	const result = await response.json();
	if (!result.success) { alert(result.message || "Failed to open chat scan area picker."); return; }
	let refreshes = 0;
	const timer = setInterval(async function() { await loadChatTextROI(); if (++refreshes >= 30) clearInterval(timer); }, 1000);
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

function syncSkillPresentation(clickMode) {
	const groups = [
		{ slots: ["1", "2", "3", "4", "5", "6", "7", "8", "9", "0"], prefix: "Attack" },
		{ slots: ["F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10"], prefix: "Buff" }
	];

	for (const group of groups) {
		group.slots.forEach(function(slot, index) {
			const skill = document.getElementById("skill" + slot);
			if (!skill) return;
			const label = skill.closest(".skill-card").querySelector(".skill-name");
			if (label) label.textContent = clickMode ? group.prefix + " " + (index + 1) : slot;
		});
	}

	document.getElementById("normalNumberSkillsTitle").textContent = clickMode ? "Attack Skills" : "1 - 0";
	document.getElementById("normalFunctionSkillsTitle").textContent = clickMode ? "Buff Skills" : "F1 - F10";
	const buffHint = document.querySelector("#normalFunctionSkillsSection > .hint");
	if (buffHint) {
		buffHint.textContent = clickMode
			? "Set a click area and repeat interval for each Buff Skill."
		: "F1 - F10 pre-fire ~300ms early and always win timing clashes against 1 - 0.";
	}
}

function syncTargetMode() {
	ensureClickTargetToggles();
	const windowReady = hasSelectedTargetWindow();
	const clickMode = getTargetInputMethod() === "click";
	const popupClickMode = usesPopupClickMethod();
	// Both Party Invite actions use their own saved pointers in every input
	// method; Keyboard Method changes combat skills only, not popup actions.
	const partyInviteAccept = document.getElementById("partyInviteAccept");
	const partyInviteReject = document.getElementById("partyInviteReject");
	partyInviteAccept.disabled = !windowReady;
	partyInviteReject.disabled = !windowReady;
	syncSkillPresentation(clickMode);
	const clickWhitelistMode = clickMode && isClickWhitelistMode();
	document.body.classList.toggle("click-whitelist-mode", clickWhitelistMode);
	document.body.classList.toggle("click-input-mode", clickMode);
	const untilRadio = document.getElementById("targetUntilDead");
	untilRadio.disabled = false;
	const mode = getTargetMode();
	const supportMode = mode === "until" && getTargetUntilRole() === "support";
	document.getElementById("targetPanelAreaSection").hidden = clickMode && !clickWhitelistMode;
	document.getElementById("clickTargetBehavior").hidden = !clickMode;
	document.getElementById("clickWhitelistNamesSettings").hidden = !clickWhitelistMode;
	document.getElementById("clickWhitelistTargetNames").disabled = !windowReady || !clickWhitelistMode;
	document.getElementById("keyboardActionsGrid").hidden = clickMode;
	document.getElementById("autoPotClickHint").hidden = !clickMode;
	const globalClickAreas = document.getElementById("clickGlobalActionAreas");
	if (globalClickAreas) globalClickAreas.hidden = !popupClickMode;
	const clickPopupScans = document.getElementById("clickPopupScanAreas");
	if (clickPopupScans) clickPopupScans.hidden = !popupClickMode;
	const chatTextSettings = document.getElementById("chatTextClickSettings");
	if (chatTextSettings) chatTextSettings.hidden = false;
	document.getElementById("chatTextClickEnabled").disabled = !windowReady;
	document.getElementById("chatTextClickKeyword").disabled = !windowReady;
	document.getElementById("chatTextClickYOffset").disabled = !windowReady;
	document.getElementById("autoPotHPSlot").hidden = clickMode;
	document.getElementById("autoPotTPSlot").hidden = clickMode;
	for (const method of document.querySelectorAll('input[name="targetInputMethod"]')) {
		method.disabled = !windowReady;
	}
	document.getElementById("skillInputHint").textContent = clickMode
		? "Select from slots 1–0 and F1–F10, set each click area, and enter its repeat interval in seconds."
		: "Check the skills to run and enter each interval in seconds. For this test, only slots 1–5 are enabled.";
	if (clickWhitelistMode) {
		document.getElementById("skillInputHint").textContent = "Target? slots repeat while searching; unchecked slots stay held until the target name matches the whitelist. Each slot uses its own click area and interval.";
	} else if (clickMode) {
		document.getElementById("skillInputHint").textContent = "Set a click area and repeat interval for each Attack Skill.";
	} else if (!clickMode) {
		document.getElementById("skillInputHint").textContent = "Check any slots 1–0 or F1–F10 to run and enter each interval in seconds.";
	}
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
	document.getElementById("targetSupportSkills").hidden = clickMode || !supportMode;
	document.getElementById("normalNumberSkillsSection").hidden = !clickMode && supportMode;
	document.getElementById("normalFunctionSkillsSection").hidden = !clickMode && supportMode;
	for (const extraSkill of document.querySelectorAll(".click-only-skill")) {
		extraSkill.hidden = !clickMode && supportMode;
	}
	// Popup and Party actions retain their virtual-click controls in every
	// method. Combat click areas remain exclusive to Click Method, so Keyboard
	// and Keypress keep the normal 1-0 / F1-F10 key-based skill controls.
	const popupAndPartyClickControls = new Set([
		"clickAreaControlsAutoAccept",
		"clickAreaControlsAutoReject",
		"clickAreaControlsAutoResu",
		"clickAreaControlsDCOk",
		"clickAreaControlsChatParty",
		"clickAreaControlsPartyAutomatic",
		"chatTextInviteControls",
	]);
	for (const controls of document.querySelectorAll(".click-area-controls")) {
		const isPopupScanControl = !!controls.closest("#clickPopupScanAreas");
		const isAutoLoginControl = !!controls.closest("#autoLoginPointerAreas");
		// The scan-area controls are nested inside #chatTextInviteControls; the
		// wrapper itself is not a .click-area-controls element.
		const isSharedChatInviteControl = !!controls.closest("#chatTextInviteControls") || controls.id === "clickAreaControlsChatParty";
		controls.hidden = !isAutoLoginControl && !isSharedChatInviteControl && !clickMode && !(popupClickMode && (popupAndPartyClickControls.has(controls.id) || isPopupScanControl));
	}
	document.getElementById("attackActionCard").hidden = clickMode;
	document.getElementById("pickActionCard").hidden = clickMode;
	for (const skillCheck of document.querySelectorAll(".skill-check")) {
		skillCheck.disabled = !windowReady || (!clickMode && supportMode);
	}
	for (const skillDelay of document.querySelectorAll(".skill-delay")) {
		skillDelay.hidden = false;
		skillDelay.disabled = !windowReady || (!clickMode && supportMode);
	}
	for (const toggle of document.querySelectorAll(".click-target-toggle")) {
		toggle.hidden = !clickWhitelistMode;
		toggle.querySelector("input").disabled = !windowReady || !clickWhitelistMode;
	}
	for (const behavior of document.querySelectorAll('input[name="clickTargetBehavior"]')) {
		behavior.disabled = !windowReady || !clickMode;
	}
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
	document.getElementById("attackActionCard").hidden = clickMode;
	document.getElementById("pickActionCard").hidden = clickMode;
	document.getElementById("attack").disabled = !windowReady || supportMode;
	document.getElementById("attackDelay").disabled = !windowReady || supportMode;
	document.getElementById("pick").disabled = !windowReady;
	document.getElementById("pickDelay").disabled = !windowReady;
	document.getElementById("pickDelay").hidden = false;
	syncSupportSkillSlots();
	syncEmergencySkillSlots();
	loadClickSkillAreas();
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

async function loadPartyAutomaticPreview() {
	const preview = document.getElementById("partyAutomaticROIPreview");
	if (!preview) return;
	try {
		const response = await fetch("/api/party-automatic/preview", { cache: "no-store" });
		if (!response.ok) {
			preview.style.display = "none";
			preview.removeAttribute("src");
			return;
		}
		preview.src = "/api/party-automatic/preview?t=" + Date.now();
		preview.style.display = "block";
	} catch (_) {
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
	restoreTheme();
	installROIControls();
	buildEmergencySkills();
	buildSupportSkills();
	updateWindowDependentControls();
	await loadGameLauncher();
	await loadWindows();
	loadPartyAutomaticPreview();
	loadTargetROI();
	loadChatTextROI();
	loadClickSkillAreas();
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
	const clickSkillScheduleChanged = getTargetInputMethod() === "click" &&
		(event.target.classList.contains("skill-check") || event.target.classList.contains("skill-delay"));
	if (clickSkillScheduleChanged) applyLiveClickSkills();
	if (event.target.name === "targetMode" || event.target.name === "targetUntilRole" || event.target.name === "targetNameFilterMode") {
		syncTargetMode();
	}
	if (event.target.name === "targetInputMethod") {
		syncTargetMode();
		loadClickSkillAreas();
		loadClickPopupScanAreas();
	}
	if (event.target.name === "clickTargetBehavior") {
		syncTargetMode();
	}
	if (event.target.id === "autoPauseDeath") loadClickPopupScanAreas();
	if (event.target.id === "autoPotHP" || event.target.classList.contains("emergency-check") || event.target.classList.contains("emergency-needs-target")) {
		syncEmergencySkillSlots();
	}
	if (event.target.classList.contains("support-skill-check")) {
		syncSupportSkillSlots();
	}
	if (event.target.id !== "windowSelect" && event.target.id !== "gameExecutableSelect" && event.target.id !== "autoLoginEnabled" && !clickSkillScheduleChanged) {
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
	HWND      string           `json:"hwnd"`
	AutoLogin GameLaunchConfig `json:"autoLogin"`

	AutoAcceptEnabled            bool    `json:"autoAcceptEnabled"`
	PartyInviteAction            string  `json:"partyInviteAction"`
	AutoPauseDeathEnabled        bool    `json:"autoPauseDeathEnabled"`
	AutoResurrectEnabled         bool    `json:"autoResurrectEnabled"`
	AssistSkillVK                uintptr `json:"assistSkillVK"`
	AssistSkillDelay             float64 `json:"assistSkillDelay"`
	TargetUntilDeadEnabled       bool    `json:"targetUntilDeadEnabled"`
	TargetUntilDeadSupport       bool    `json:"targetUntilDeadSupport"`
	TargetUntilDeadCharacterName string  `json:"targetUntilDeadCharacterName"`
	TargetNameFilterMode         string  `json:"targetNameFilterMode"`
	ClickWhitelistMode           bool    `json:"clickWhitelistMode"`
	ClickWhitelistTargetNames    string  `json:"clickWhitelistTargetNames"`
	ChatTextClickEnabled         bool    `json:"chatTextClickEnabled"`
	ChatTextClickKeyword         string  `json:"chatTextClickKeyword"`
	ChatTextClickYOffset         int     `json:"chatTextClickYOffset"`

	TargetEnabled     bool    `json:"targetEnabled"`
	InputMethod       string  `json:"inputMethod"`
	TargetWithClick   bool    `json:"targetWithClick"`
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
	// separate 1–5 profile selected only by Target Until Dead > Support, so
	// switching roles never overwrites an existing working attacker setup.
	Skills        []WebSkillConfig        `json:"skills"`
	SupportSkills []WebSupportSkillConfig `json:"supportSkills"`
}

// ClickMethod is the global input selection. TargetWithClick remains as a
// legacy fallback so existing saved profiles continue to work.
func (cfg WebBotConfig) ClickMethod() bool {
	switch strings.ToLower(strings.TrimSpace(cfg.InputMethod)) {
	case "click":
		return true
	case "keyboard", "keypress":
		return false
	default:
		return cfg.TargetWithClick
	}
}

// KeypressMethod sends combat keys through SendInput. It deliberately remains
// distinct from ClickMethod: skills and potions use keys.
func (cfg WebBotConfig) KeypressMethod() bool {
	return strings.EqualFold(strings.TrimSpace(cfg.InputMethod), "keypress")
}

// PopupClickMethod is shared by every normalized input method. Keyboard
// Method changes combat-key delivery only; party, resurrection, DC, and chat
// popups always use the saved click pointers and scan areas.
func (cfg WebBotConfig) PopupClickMethod() bool {
	return true
}

func normalizeInputMethod(cfg *WebBotConfig) {
	if cfg == nil {
		return
	}
	if cfg.ClickMethod() {
		cfg.InputMethod = "click"
		cfg.TargetWithClick = true
	} else if cfg.KeypressMethod() {
		cfg.InputMethod = "keypress"
		cfg.TargetWithClick = false
	} else {
		cfg.InputMethod = "keyboard"
		cfg.TargetWithClick = false
	}
}

// usesTargetNameFilter is deliberately false for Target without name even
// when a previous filtered profile left names in the text box. This mode is a
// key-only target loop: it must neither need a Target ROI nor hold skills for
// target-name OCR.
func usesTargetNameFilter(cfg WebBotConfig) bool {
	if cfg.ClickMethod() {
		return cfg.ClickWhitelistMode
	}
	withoutName := cfg.TargetWithoutName || strings.EqualFold(strings.TrimSpace(cfg.TargetNameFilterMode), "none")
	return cfg.TargetUntilDeadEnabled ||
		(cfg.TargetEnabled && !withoutName && strings.TrimSpace(cfg.TargetUntilDeadCharacterName) != "")
}

func usesTargetBarMonitor(cfg WebBotConfig) bool {
	return usesTargetNameFilter(cfg)
}

func targetNameFilterValues(cfg WebBotConfig) (string, string) {
	if cfg.ClickMethod() && cfg.ClickWhitelistMode {
		return cfg.ClickWhitelistTargetNames, targetNameFilterModeWhitelist
	}
	return cfg.TargetUntilDeadCharacterName, cfg.TargetNameFilterMode
}

func requiresTargetNameWhitelist(cfg WebBotConfig) bool {
	if cfg.ClickMethod() {
		return cfg.ClickWhitelistMode && strings.TrimSpace(cfg.ClickWhitelistTargetNames) == ""
	}
	withoutName := cfg.TargetWithoutName || strings.EqualFold(strings.TrimSpace(cfg.TargetNameFilterMode), "none")
	usesNameList := cfg.TargetUntilDeadEnabled || (cfg.TargetEnabled && !withoutName)
	return usesNameList &&
		normalizeTargetNameFilterMode(cfg.TargetNameFilterMode) == targetNameFilterModeWhitelist &&
		strings.TrimSpace(cfg.TargetUntilDeadCharacterName) == ""
}

type WebEmergencySkillConfig struct {
	Index                int     `json:"index"`
	Enabled              bool    `json:"enabled"`
	VK                   uintptr `json:"vk"`
	NeedsTarget          bool    `json:"needsTarget"`
	Click                bool    `json:"-"`
	ClickAreaSet         bool    `json:"-"`
	ClickX               int     `json:"-"`
	ClickY               int     `json:"-"`
	ClickReferenceWidth  int     `json:"-"`
	ClickReferenceHeight int     `json:"-"`
}

type WebSkillConfig struct {
	Name         string  `json:"name"`
	VK           uintptr `json:"vk"`
	Enabled      bool    `json:"enabled"`
	Delay        float64 `json:"delay"`
	TargetSearch bool    `json:"targetSearch"`
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
	if cfg.PopupClickMethod() && !LoadClickPopupScanAreas()["death"].ROI.Selected {
		return "Select the Click Death / Resu scan area before enabling Auto Resu."
	}
	if !cfg.PopupClickMethod() && !LoadDeathROI().Selected {
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
	if cfg.AssistSkillVK != 0 && !cfg.ClickMethod() {
		if message := validPositive(cfg.AssistSkillDelay, "Assist skill interval"); message != "" {
			return message
		}
	}
	if !cfg.ClickMethod() && cfg.TargetEnabled {
		if message := validPositive(cfg.TargetDelay, "Target interval"); message != "" {
			return message
		}
	}
	if !cfg.ClickMethod() && cfg.AttackEnabled && !(cfg.TargetUntilDeadEnabled && cfg.TargetUntilDeadSupport) {
		if message := validPositive(cfg.AttackDelay, "Attack interval"); message != "" {
			return message
		}
	}
	if !cfg.ClickMethod() && cfg.PickEnabled {
		if message := validPositive(cfg.PickDelay, "Pick interval"); message != "" {
			return message
		}
	}
	activeSkills := cfg.Skills
	if !cfg.ClickMethod() && cfg.TargetUntilDeadEnabled && cfg.TargetUntilDeadSupport {
		for _, skill := range cfg.SupportSkills {
			if skill.Enabled {
				if skill.VK < 0x31 || skill.VK > 0x35 {
					return "Support Skills may use only 1 through 5."
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

func validateClickSkillAreas(cfg WebBotConfig) string {
	if !cfg.ClickMethod() {
		return ""
	}
	areas := LoadClickSkillROIs()
	for _, skill := range cfg.Skills {
		if !skill.Enabled || !isClickSkillVK(skill.VK) {
			continue
		}
		if roi, ok := areas[skill.Name]; !ok || !roi.Selected {
			return "Set the click area for Skill " + skill.Name + " before starting."
		}
	}
	return ""
}

func validateClickEmergencyAreas(cfg WebBotConfig) string {
	if !cfg.ClickMethod() {
		return ""
	}
	areas := LoadClickSkillROIs()
	for index, skill := range cfg.EmergencySkills {
		if !skill.Enabled {
			continue
		}
		slot := skill.Index
		if slot < 1 || slot > emergencySkillSlotCount {
			slot = index + 1
		}
		key := fmt.Sprintf("Emergency%d", slot)
		if roi, ok := areas[key]; !ok || !roi.Selected {
			return fmt.Sprintf("Set the click area for Emergency Skill %d before starting.", slot)
		}
	}
	return ""
}

func validateAutoPotClickArea(cfg WebBotConfig) string {
	if !cfg.ClickMethod() {
		return ""
	}
	areas := LoadClickSkillROIs()
	if cfg.AutoPotHPEnabled && !areas["HP"].Selected {
		return "Set the HP Pot click area before starting."
	}
	if cfg.AutoPotTPEnabled && !areas["TP"].Selected {
		return "Set the TP Pot click area before starting."
	}
	return ""
}

func validateClickActionAreas(cfg WebBotConfig) string {
	areas := LoadClickSkillROIs()
	if cfg.AutoAcceptEnabled {
		slot := "AutoAccept"
		label := "Accept"
		if cfg.PartyInviteSelection() == partyInviteReject {
			slot = "AutoReject"
			label = "Reject"
		}
		if !areas[slot].Selected {
			return "Set the Party Invite " + label + " pointer before starting."
		}
	}
	if !cfg.PopupClickMethod() {
		return ""
	}
	if cfg.AutoResurrectEnabled && !areas["AutoResu"].Selected {
		return "Set the Auto Resu click area before starting."
	}
	// The DC dialog may still be monitored in Click Method, but configuring
	// its OK point is optional. If unset, acknowledgement simply cannot click.
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
	action  string
}

func NewAutoAcceptController(
	enabled bool,
	action string,
) *AutoAcceptController {

	return &AutoAcceptController{
		enabled: enabled,
		action:  normalizedPartyInviteAction(action),
	}
}

func (a *AutoAcceptController) IsEnabled() bool {

	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.enabled
}

func (a *AutoAcceptController) Set(
	enabled bool,
	action string,
) {

	a.mu.Lock()
	defer a.mu.Unlock()

	a.enabled = enabled
	a.action = normalizedPartyInviteAction(action)
}

func (a *AutoAcceptController) Action() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.action
}

const (
	partyInviteAccept = "accept"
	partyInviteReject = "reject"
)

func normalizedPartyInviteAction(action string) string {
	if strings.EqualFold(strings.TrimSpace(action), partyInviteReject) {
		return partyInviteReject
	}
	return partyInviteAccept
}

func (cfg WebBotConfig) PartyInviteSelection() string {
	return normalizedPartyInviteAction(cfg.PartyInviteAction)
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
	// GAME LAUNCHER
	// ========================================================

	mux.HandleFunc("/api/game-launch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		cfg, err := loadGameLaunchConfig()
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		executables := []string{}
		if cfg.Folder != "" {
			if executables, err = listGameExecutables(cfg.Folder); err != nil {
				cfg.EXE = ""
				executables = []string{}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":     true,
			"config":      cfg,
			"executables": executables,
			"dcScanReady": autoLoginDCScanConfigured(),
		})
	})

	mux.HandleFunc("/api/game-launch/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			Config GameLaunchConfig `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error())
			return
		}
		if err := setGameLaunchConfig(request.Config); err != nil {
			writeJSONError(w, err.Error())
			return
		}
		cfg, err := loadGameLaunchConfig()
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "config": cfg})
	})

	mux.HandleFunc("/api/game-launch/pointer", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			Slot    string        `json:"slot"`
			Pointer ClickSkillROI `json:"pointer"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error())
			return
		}
		cfg, err := setGameLaunchPointer(request.Slot, request.Pointer)
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "config": cfg})
	})

	mux.HandleFunc("/api/game-launch/pointer/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			Slot string `json:"slot"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error())
			return
		}
		cfg, err := resetGameLaunchPointer(request.Slot)
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "config": cfg})
	})

	mux.HandleFunc("/api/game-launch/folder", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		folder, selected, err := pickGameFolder()
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if !selected {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "cancelled": true})
			return
		}
		cfg, executables, err := selectGameLaunchFolder(folder)
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":     true,
			"config":      cfg,
			"executables": executables,
		})
	})

	mux.HandleFunc("/api/game-launch/executable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			EXE string `json:"exe"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error())
			return
		}
		cfg, err := selectGameLaunchExecutable(request.EXE)
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "config": cfg})
	})

	mux.HandleFunc("/api/game-launch/enabled", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error())
			return
		}
		cfg, err := setGameLaunchEnabled(request.Enabled)
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "config": cfg})
	})

	mux.HandleFunc("/api/game-launch/auto-hunt", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			RunAutoHunt bool `json:"runAutoHunt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error())
			return
		}
		cfg, err := setGameLaunchAutoHunt(request.RunAutoHunt)
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "config": cfg})
	})

	mux.HandleFunc("/api/game-launch/auto-hunt-key", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			AutoHuntKey string `json:"autoHuntKey"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error())
			return
		}
		cfg, err := setGameLaunchAutoHuntKey(request.AutoHuntKey)
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "config": cfg})
	})

	mux.HandleFunc("/api/game-launch/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		cfg, err := launchConfiguredGame()
		if err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "config": cfg})
	})

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
				runtimeManager.StopWithReason(stopReasonAppExit)
				runtimeManager.CloseTelemetry()
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

	// Preview of the compact Normal / Automatic OCR selection. This uses the
	// same picker crop the runtime will later read and click in its centre.
	mux.HandleFunc("/api/party-automatic/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		preview, ok := getPartyAutomaticROIPreview()
		if !ok {
			http.Error(w, "no selected-area preview yet", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(preview)
	})

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

	mux.HandleFunc("/api/click-popup-scan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		kind := r.URL.Query().Get("kind")
		if !validClickPopupScanKind(kind) {
			writeJSONError(w, "Unknown Click popup scan area")
			return
		}
		area := LoadClickPopupScanAreas()[kind]
		if hwnd, err := parseHWND(r.URL.Query().Get("hwnd")); err == nil && hwnd != 0 {
			if clientRect, rectErr := getPickerWindowRect(hwnd); rectErr == nil {
				if mapped, ok := LoadClickPopupScanAreaForClient(kind,
					int(clientRect.Right-clientRect.Left), int(clientRect.Bottom-clientRect.Top)); ok {
					area = mapped
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true, "selected": area.ROI.Selected, "custom": area.Custom,
			"x": area.ROI.X, "y": area.ROI.Y, "w": area.ROI.Width, "h": area.ROI.Height,
		})
	})

	mux.HandleFunc("/api/click-popup-scan/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		kind := r.URL.Query().Get("kind")
		if !validClickPopupScanKind(kind) {
			http.Error(w, "unknown Click popup scan area", http.StatusBadRequest)
			return
		}
		preview, ok := getClickPopupScanROIPreview("click-popup:" + kind)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(preview)
	})

	mux.HandleFunc("/api/click-popup-scan/picker", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			HWND string `json:"hwnd"`
			Kind string `json:"kind"`
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
		if err := openClickPopupScanROIPicker(windows.Handle(hwnd), request.Kind); err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	})

	mux.HandleFunc("/api/click-popup-scan/reset", func(w http.ResponseWriter, r *http.Request) {
		kind := r.URL.Query().Get("kind")
		if !validClickPopupScanKind(kind) {
			writeJSONError(w, "Unknown Click popup scan area")
			return
		}
		handleROIReset(w, r, "click-popup-"+kind)
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

	mux.HandleFunc("/api/chat-text-roi", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		area := LoadChatTextROI()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "selected": area.ROI.Selected, "x": area.ROI.X, "y": area.ROI.Y, "w": area.ROI.Width, "h": area.ROI.Height})
	})
	mux.HandleFunc("/api/chat-text-roi/picker", func(w http.ResponseWriter, r *http.Request) {
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
		if err := openChatTextROIPicker(windows.Handle(hwnd)); err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	})
	mux.HandleFunc("/api/chat-text-roi/reset", func(w http.ResponseWriter, r *http.Request) { handleROIReset(w, r, "chat-text") })

	mux.HandleFunc("/api/click-skill/areas", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "areas": LoadClickSkillROIs()})
	})

	mux.HandleFunc("/api/click-skill/picker", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			HWND string `json:"hwnd"`
			Slot string `json:"slot"`
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
		if err := openClickSkillROIPicker(windows.Handle(hwnd), request.Slot); err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	})

	mux.HandleFunc("/api/click-skill/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			Slot string `json:"slot"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error())
			return
		}
		if err := ResetPickedClickSkillROI(request.Slot); err != nil {
			writeJSONError(w, err.Error())
			return
		}
		if request.Slot == "PartyAutomatic" {
			clearROIPreview("party-automatic")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
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
			normalizeInputMethod(&request.Config)
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

	mux.HandleFunc("/api/config/click-skills", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var cfg WebBotConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error())
			return
		}
		normalizeInputMethod(&cfg)
		if err := runtimeManager.UpdateClickSkills(cfg); err != nil {
			writeJSONError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Click skills applied live."})
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
			normalizeInputMethod(&cfg)
			if message := validateNumericConfig(cfg); message != "" {
				writeJSONError(w, message)
				return
			}
			if message := validateAutoPotClickArea(cfg); message != "" {
				writeJSONError(w, message)
				return
			}
			if message := validateClickActionAreas(cfg); message != "" {
				writeJSONError(w, message)
				return
			}
			if message := validateChatTextClickConfig(cfg); message != "" {
				writeJSONError(w, message)
				return
			}

			if cfg.AutoAcceptEnabled && ((!cfg.PopupClickMethod() && !ocrworker.LoadPartyROI().Selected) || (cfg.PopupClickMethod() && !LoadClickPopupScanAreas()["party"].ROI.Selected)) {
				writeJSONError(w, "Select Party OCR area before enabling Auto Party Invite.")
				return
			}
			if (cfg.AutoPotHPEnabled || cfg.AutoPotTPEnabled) && !LoadStatusROI().Selected {
				writeJSONError(w, "Select the HP / TP status area before enabling Auto Potion.")
				return
			}
			if cfg.AutoPauseDeathEnabled && ((!cfg.PopupClickMethod() && !LoadDeathROI().Selected) || (cfg.PopupClickMethod() && !LoadClickPopupScanAreas()["death"].ROI.Selected)) {
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
			if usesTargetBarMonitor(cfg) && !LoadTargetROI().Selected {
				writeJSONError(w, "Select the target name and HP bar before using Whitelist Target, Target Name Filter, or Target Until Dead.")
				return
			}
			if requiresTargetNameWhitelist(cfg) {
				writeJSONError(w, "Enter one or more whitelist target names before enabling this mode.")
				return
			}
			if !cfg.ClickMethod() && cfg.TargetUntilDeadEnabled && strings.TrimSpace(cfg.TargetUntilDeadCharacterName) == "" {
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
			normalizeInputMethod(&cfg)
			if message := validateNumericConfig(cfg); message != "" {
				writeJSONError(w, message)
				return
			}
			if message := validateChatTextClickConfig(cfg); message != "" {
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

			normalizeInputMethod(&cfg)

			if message := validateNumericConfig(cfg); message != "" {

				writeJSONError(w, message)

				return
			}

			if message := validateClickSkillAreas(cfg); message != "" {
				writeJSONError(w, message)
				return
			}
			if message := validateClickEmergencyAreas(cfg); message != "" {
				writeJSONError(w, message)
				return
			}
			if message := validateAutoPotClickArea(cfg); message != "" {
				writeJSONError(w, message)
				return
			}
			if message := validateClickActionAreas(cfg); message != "" {
				writeJSONError(w, message)
				return
			}

			if cfg.AutoAcceptEnabled && ((!cfg.PopupClickMethod() && !ocrworker.LoadPartyROI().Selected) || (cfg.PopupClickMethod() && !LoadClickPopupScanAreas()["party"].ROI.Selected)) {
				writeJSONError(
					w,
					"Select Party OCR area before enabling Auto Party Invite.",
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

			if cfg.AutoPauseDeathEnabled && ((!cfg.PopupClickMethod() && !LoadDeathROI().Selected) || (cfg.PopupClickMethod() && !LoadClickPopupScanAreas()["death"].ROI.Selected)) {
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

			if usesTargetBarMonitor(cfg) && !LoadTargetROI().Selected {
				writeJSONError(
					w,
					"Select the target name and HP bar before using Whitelist Target, Target Name Filter, or Target Until Dead.",
				)
				return
			}

			if requiresTargetNameWhitelist(cfg) {
				writeJSONError(
					w,
					"Enter one or more whitelist target names before enabling this mode.",
				)
				return
			}

			if !cfg.ClickMethod() && cfg.TargetUntilDeadEnabled && strings.TrimSpace(cfg.TargetUntilDeadCharacterName) == "" {
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

	autoAccept.Set(
		cfg.AutoAcceptEnabled,
		cfg.PartyInviteSelection(),
	)

	autoPot.Update(cfg)
	autoPot.SetClickSender(func(hwnd uintptr, x, y, refW, refH int) bool {
		return bot.CastPriorityClickAction("AutoPot", hwnd, x, y, refW, refH)
	})
	autoPot.SetKeySender(func(vk uintptr) bool {
		return bot.CastPriorityKeyAction("AutoPot", vk)
	})
	emergency.SetInputSender(bot.CastEmergencySkill)
	clickAreas := LoadClickSkillROIs()
	for index := range cfg.EmergencySkills {
		slot := &cfg.EmergencySkills[index]
		if slot.Index < 1 || slot.Index > emergencySkillSlotCount {
			slot.Index = index + 1
		}
		if cfg.ClickMethod() {
			area := clickAreas[fmt.Sprintf("Emergency%d", slot.Index)]
			slot.Click = true
			slot.ClickAreaSet = area.Selected
			slot.ClickX = area.X + area.Width/2
			slot.ClickY = area.Y + area.Height/2
			slot.ClickReferenceWidth = area.ClientWidth
			slot.ClickReferenceHeight = area.ClientHeight
		}
	}
	emergency.Update(cfg)
	deathPause.Update(cfg.AutoPauseDeathEnabled, cfg.AutoResurrectEnabled)
	targetFilterEnabled := usesTargetNameFilter(cfg)
	supportMode := !cfg.ClickMethod() && cfg.TargetUntilDeadEnabled && cfg.TargetUntilDeadSupport
	targetNames, targetFilterMode := targetNameFilterValues(cfg)
	targetUntil.Update(
		cfg.TargetUntilDeadEnabled && !cfg.ClickMethod(),
		targetFilterEnabled,
		supportMode,
		targetNames,
		targetFilterMode,
	)
	bot.SetTargetActionFilterEnabled(targetFilterEnabled)
	bot.SetTargetActionReady(!targetFilterEnabled && !cfg.ClickWhitelistMode)
	bot.SetTargetPanelClear(false)

	bot.mu.Lock()

	bot.config.AssistEnabled =
		!cfg.ClickMethod() && !cfg.TargetEnabled && !cfg.TargetUntilDeadEnabled && cfg.AssistSkillVK != 0

	bot.config.AssistSkillVK =
		cfg.AssistSkillVK

	bot.config.AssistSkillDelay =
		secondsToDuration(
			cfg.AssistSkillDelay,
		)

	bot.config.TargetEnabled =
		!cfg.ClickMethod() && cfg.TargetEnabled && !cfg.TargetUntilDeadEnabled

	bot.config.TargetDelay =
		secondsToDuration(
			cfg.TargetDelay,
		)

	// R is an attacker action. Preserve its checkbox and saved value for the
	// Attacker profile, but never schedule it while the Support profile owns
	// Target Until Dead.
	bot.config.AttackEnabled =
		!cfg.ClickMethod() && cfg.AttackEnabled && !supportMode

	bot.config.AttackDelay =
		secondsToDuration(
			cfg.AttackDelay,
		)

	bot.config.PickEnabled =
		!cfg.ClickMethod() && cfg.PickEnabled

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
	bot.config.ClickMode = cfg.ClickMethod()
	bot.config.KeypressMode = cfg.KeypressMethod()
	bot.config.ClickWhitelistMode = cfg.ClickMethod() && cfg.ClickWhitelistMode
	if supportMode {
		for _, skill := range cfg.SupportSkills {
			if skill.VK < 0x31 || skill.VK > 0x35 {
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
			if !isClickSkillVK(skill.VK) {
				continue
			}

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
						Click:                cfg.ClickMethod(),
						ClickAreaSet:         clickAreas[skill.Name].Selected,
						ClickX:               clickAreas[skill.Name].X + clickAreas[skill.Name].Width/2,
						ClickY:               clickAreas[skill.Name].Y + clickAreas[skill.Name].Height/2,
						ClickReferenceWidth:  clickAreas[skill.Name].ClientWidth,
						ClickReferenceHeight: clickAreas[skill.Name].ClientHeight,
						TargetSearch:         cfg.ClickMethod() && cfg.ClickWhitelistMode && skill.TargetSearch,
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
	previewKind := kind
	switch kind {
	case "party":
		err = ocrworker.ResetPickedPartyROI()
	case "status":
		err = ResetPickedStatusROI()
	case "death":
		err = ResetPickedDeathROI()
	case "target":
		err = ResetPickedTargetROI()
	case "chat-text":
		err = ResetPickedChatTextROI()
	case "click-popup-party", "click-popup-death", "click-popup-dc":
		popupKind := strings.TrimPrefix(kind, "click-popup-")
		err = ResetClickPopupScanArea(popupKind)
		previewKind = "click-popup:" + popupKind
	default:
		writeJSONError(w, "Unknown ROI kind")
		return
	}
	if err != nil {
		writeJSONError(w, "Failed to reset selected area: "+err.Error())
		return
	}

	clearROIPreview(previewKind)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func validateChatTextClickConfig(cfg WebBotConfig) string {
	if !cfg.ChatTextClickEnabled {
		return ""
	}
	if cfg.ChatTextClickYOffset < 0 || cfg.ChatTextClickYOffset > 200 {
		return "Click-below-text offset must be between 0 and 200 pixels."
	}
	if strings.TrimSpace(cfg.ChatTextClickKeyword) == "" {
		return "Enter the chat keyword for Party Invite."
	}
	if !LoadChatTextROI().ROI.Selected {
		return "Set the chat text scan area before enabling Chat Keyword Party Invite."
	}
	if area, ok := LoadClickSkillROIs()["ChatParty"]; !ok || !area.Selected {
		return "Set the Party action pointer before enabling Chat Keyword Party Invite."
	}
	return ""
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
