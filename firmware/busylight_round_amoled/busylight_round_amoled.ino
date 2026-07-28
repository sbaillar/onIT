/*
 * Teams Busylight — Theme 1e "TEAMS", 1.75" AMOLED edition
 * Waveshare ESP32-S3-Touch-AMOLED-1.75 (CO5300 466x466 QSPI round, CST9217 touch)
 *
 * Libraries: "GFX Library for Arduino" (moononournation Arduino_GFX, CO5300 driver)
 *            "NimBLE-Arduino" (h2zero, 2.x API)
 *            "SensorLib" (lewisxhe, TouchDrvCST92xx)
 * Board:     ESP32S3 Dev Module. USB CDC On Boot MUST be enabled (this board
 *            has no USB-UART bridge; with it off Serial goes to UART0's pins
 *            and the device is mute over USB) — the Makefile FQBN sets it.
 *
 * Serial in : STATE:available|meeting|sharing|flashing|off   @115200
 *             STATE:custom:<text>       (yellow screen, text auto-fitted)
 *             STATE:custom:RRGGBB,RRGGBB:<text>  (background,font colors)
 *             EMOJI:<base64>            (120x120 RGB565 LE image, pixel-
 *             quadrupled to fill the screen; shown
 *             immediately and kept alive by STATE:emoji heartbeats)
 *             SPIN                       (start the emoji roulette)
 *             DECKIMG:<slot>:<last>:<base64>  (store one roulette-deck image;
 *             acked with DECKOK:<slot> so the host paces the upload)
 *             DECKSIG:<sig> / DECKSIG    (set / query the signature of the
 *             deck now on the device, so a resync can be skipped)
 *             VERSION                    (query firmware version)
 * Serial out: VERSION:x.y.z:amoled175  (at boot and on VERSION query)
 *             TOUCH:TAP / TOUCH:LONG   (screen tapped / long-pressed;
 *             the host decides what they mean)
 *             ROULETTE:<slot>          (the roulette wheel settled on <slot>)
 *             DECKOK:<slot> / DECKSIG:<sig>  (deck upload ack / deck query reply)
 *
 * BLE       : NimBLE GATT server, one service, three characteristics, all
 *             requiring an encrypted bonded link (LE Secure Connections,
 *             DisplayOnly — 6-digit passkey shown full-screen while pairing):
 *               Command (write)  same text lines as serial (STATE:*, SPIN,
 *                                VERSION, TIME:<epoch>) plus config lines
 *                                TZ:<posix-tz> (stored in NVS) and
 *                                DECK:<count>.
 *               Emoji   (write)  v2 10-byte header (offset u32, total u16,
 *                                seq u16, slot u8, flags u8, little-endian) +
 *                                raw RGB565 chunk (<=502 B); reassembled by
 *                                offset, discarded on disconnect or 2 s
 *                                inter-chunk timeout. total must be 28800.
 *                                slot 0xFF = display now; slot 0..19 = store
 *                                into deck slot N in LittleFS (/deck/N.rgb);
 *                                flags bit0 = last chunk of a deck sync.
 *               Events  (notify) TOUCH:TAP / TOUCH:LONG / ROULETTE:<slot> /
 *                                VERSION:x.y.z:amoled175
 *             Advertised name onIT-AMOLED-<last4 of BT MAC>. Pairable only the
 *             first 5 minutes after boot or while in pairing mode, entered by a
 *             10 s finger-hold on the face (a green progress ring fills from ~2 s
 *             to 10 s; releasing early cancels it). Pairing mode shows a pairing
 *             screen + full-screen passkey and exits on success, a 2-minute
 *             timeout, or another 10 s hold. Pairing attempts outside these are
 *             rejected (disconnected).
 * Standalone: with no live host (the OFF/STALE condition) the panel shows an
 *             analog clock. Time comes from the host: a TIME:<epoch> line
 *             (UTC seconds) on every connect, displayed local through the
 *             stored POSIX TZ. While powered, timekeeping rides the 40 MHz
 *             crystal (tens of ppm — seconds per day), so one push a day
 *             keeps it honest; a power cut clears it (no battery-backed RTC)
 *             and the face stays dimmed and hand-less until the next push.
 *             A tap spins the emoji roulette (5 s ease-out through the
 *             LittleFS deck, winner stays until the next spin or a host
 *             takes over).
 * Watchdog  : USB only: no serial for 5s -> OFF/STALE (except FLASHING: sticky,
 *             shown until the flash reset - the port is closed during esptool).
 *             While BLE is connected the link itself is the liveness signal;
 *             BLE disconnect -> OFF/STALE (except FLASHING and a roulette
 *             winner, which persists standalone).
 *
 * PARTITIONS: partitions.csv next to this sketch defines the 16MB layout
 * (6.25MB app slots + a 3.3MB "spiffs"-labeled partition that LittleFS
 * formats/mounts for the emoji deck — 20 slots need 576 KB). The app now
 * fits the stock 4MB scheme too (the Wi-Fi stack is gone), but this board
 * ships 16MB and devices in the field are already laid out this way — moving
 * them to the stock table would relocate the deck partition and wipe it, for
 * no gain. Built with FlashSize=16M,PartitionScheme=custom in the FQBN
 * (PartitionScheme=custom makes arduino-cli use the sketch-local
 * partitions.csv) — the Makefile firmware target does this.
 *
 * NOTE ON PINS: values below match the Waveshare demo for this board
 * (github.com/waveshareteam/ESP32-S3-Touch-AMOLED-1.75, pin_config.h).
 * If the panel stays black, verify against
 * waveshare.com/wiki/ESP32-S3-Touch-AMOLED-1.75 for your revision.
 */

#define FW_VERSION "1.7.2"   // extracted by `make firmware`, embedded in onIT
#define BOARD_TAG  "amoled175"

#include <Arduino_GFX_Library.h>
#include <Adafruit_GFX.h>   // only for its Fonts/ include path
#include <Wire.h>
#include <NimBLEDevice.h>
#include <TouchDrv.hpp>     // SensorLib: CST9217 over I2C
#include <esp_mac.h>
#include <LittleFS.h>
#include <Preferences.h>
#include <time.h>
#include <sys/time.h>
#include <Fonts/FreeSansBold24pt7b.h>
#include <Fonts/FreeSansBold18pt7b.h>
#include <Fonts/FreeSansBold12pt7b.h>
#include <Fonts/FreeSansBold9pt7b.h>

// ---------------------------------------------------------------- pins
#define LCD_SDIO0  4
#define LCD_SDIO1  5
#define LCD_SDIO2  6
#define LCD_SDIO3  7
#define LCD_SCLK  38
#define LCD_CS    12
#define LCD_RST   39

// CST9217 touch controller (shared I2C bus)
#define TP_SDA    15
#define TP_SCL    14
#define TP_INT    11
#define TP_RST    40

#define SCREEN_W  466
#define CENTER    233
#define RING_R    222          // outer ring radius (114 scaled 240->466)
#define PROG_R    222          // hold-to-pair progress ring radius (overlays the edge)
#define PROG_W    6            // progress ring thickness (thin)

// ---------------------------------------------------------------- BLE
// "6f6e4954" = "onIT", "0175" = 1.75". Host must use the same UUIDs.
#define BLE_UUID_SVC "6f6e4954-0175-4b1e-8001-000000000001"
#define BLE_UUID_CMD "6f6e4954-0175-4b1e-8001-000000000002"
#define BLE_UUID_EMO "6f6e4954-0175-4b1e-8001-000000000003"
#define BLE_UUID_EVT "6f6e4954-0175-4b1e-8001-000000000004"

// Emoji write wire format: 10-byte header, then a raw RGB565 payload chunk.
#define EMO_HDR_LEN        10     // offset u32, total u16, seq u16, slot u8, flags u8
#define EMO_SLOT_LIVE      0xFF   // slot 0xFF = display now (else 0..DECK_MAX-1)
#define EMO_FLAG_DECK_LAST 0x01   // flags bit0 = last chunk of a deck sync

#define PAIR_WINDOW_MS (5UL * 60UL * 1000UL)   // pairable window after boot
#define PAIR_MODE_MS   (2UL * 60UL * 1000UL)   // pairing-mode auto-exit timeout
#define HOLD_LONG_MS   600UL                    // release under this = TAP, over = LONG
#define HOLD_RING_MS   2000UL                   // progress ring starts filling here
#define HOLD_PAIR_MS   10000UL                  // full hold toggles pairing mode

// ---------------------------------------------------------------- palette (RGB565 from spec)
#define C_BG_IDLE     0x1083  // #101018
#define C_GREEN       0x962A  // #90C450
#define C_RED_BUSY    0xC189  // #C03048
#define C_RED_DIM     0x4043  // #400818
#define C_RED_MRING   0xE28E  // #E05070
#define C_PURPLE      0x6335  // #6064A8
#define C_LAVENDER    0xDEDE  // #D8D8F0
#define C_WHITE       0xFFFF
#define C_BLACK       0x0000
#define C_GRAY_RING   0x4208  // #404040
#define C_GRAY_TEXT   0x5ACB  // #585858
#define C_YELLOW      0xEE09  // #E8C24A

// Presenting pulse: 8-step ring color LUT, white -> #787CB8 -> white (sine)
const uint16_t PULSE_LUT[8] = {
  0xFFFF, 0xE73C, 0xB5FA, 0x8C58, 0x7BD7, 0x8C58, 0xB5FA, 0xE73C
};

// Flashing pulse: ring #E05070 -> #400818 -> back (urgent red breathe)
const uint16_t FLASH_LUT[8] = {
  0xE28E, 0xBA4B, 0x9208, 0x4043, 0x4043, 0x9208, 0xBA4B, 0xE28E
};

// ---------------------------------------------------------------- display
Arduino_DataBus *bus = new Arduino_ESP32QSPI(
    LCD_CS, LCD_SCLK, LCD_SDIO0, LCD_SDIO1, LCD_SDIO2, LCD_SDIO3);
// The CO5300 cannot rotate: its MADCTL offers only X/Y mirroring, so the
// driver's rotation 1 and 3 are flips, not turns (1 reads mirrored, 3 looks
// unrotated). To actually turn the picture we draw into a framebuffer that
// rotates in software and push that to the panel — 466*466*2 = 434 KB, which
// lands in PSRAM (large allocations do). Canvas rotation 1 turns the picture
// 90° clockwise, which puts the USB socket at 6 o'clock.
Arduino_CO5300 *panel = new Arduino_CO5300(
    bus, LCD_RST, 0 /*panel itself unrotated*/, SCREEN_W, SCREEN_W, 6, 0, 0, 0);
Arduino_Canvas *gfx = new Arduino_Canvas(
    SCREEN_W, SCREEN_W, panel, 0, 0, 1 /*rotation: 90° clockwise*/);

TouchDrvCST92xx touch;
bool touchOk = false;

enum State { ST_OFF, ST_AVAILABLE, ST_MEETING, ST_SHARING, ST_FLASHING, ST_CUSTOM, ST_EMOJI,
             ST_ROULETTE_SPIN, ST_ROULETTE_WINNER };
State state = ST_OFF;

unsigned long lastCmd      = 0;
unsigned long lastStateChg = 0;
String customText;
uint16_t customBg = C_YELLOW, customFg = C_BLACK;
uint16_t emojiBuf[120 * 120];
bool emojiValid = false;
String lineBuf;

// ---- BLE shared state (written from NimBLE task, consumed in loop)
NimBLECharacteristic *evtChr = nullptr;
volatile int bleConns = 0;
volatile bool bleDropped = false;           // a disconnect happened
unsigned long pairableUntil = PAIR_WINDOW_MS;
volatile uint32_t pendingPasskey = 0;       // != 0 -> draw the passkey screen once
volatile uint32_t shownPasskey = 0;         // passkey currently on the panel (0 = none)
volatile bool pairingDone = false;          // auth finished -> redraw state
volatile bool pairingMode = false;          // in the 10s-hold pairing mode
unsigned long pairingModeUntil = 0;         // pairing-mode 2-minute timeout
char bleName[24] = "";                       // advertised name, shown when pairing
uint16_t lastConnHandle = BLE_HS_CONN_HANDLE_NONE;

portMUX_TYPE bleMux = portMUX_INITIALIZER_UNLOCKED;

// command queue: short text lines from the Command characteristic
#define CMD_Q_LEN 4
#define CMD_MAX   220
char cmdQ[CMD_Q_LEN][CMD_MAX];
volatile uint8_t cmdHead = 0, cmdCount = 0;

// emoji reassembly (separate rx buffer so the shown image never tears)
uint8_t emojiRx[sizeof(emojiBuf)];
volatile uint32_t emojiRxCount = 0;
volatile bool emojiRxReady = false;
volatile unsigned long emojiRxLast = 0;
volatile uint8_t emojiRxSlot = EMO_SLOT_LIVE;  // v2 header: 0xFF = display now
volatile uint8_t emojiRxFlags = 0;          // bit0 = last chunk of a deck sync

// ---- emoji deck (LittleFS /deck/N.rgb, raw 28800-byte RGB565 images)
#define DECK_MAX 20
uint8_t deckSlots[DECK_MAX];                // slot numbers with a valid file
uint8_t deckN = 0;
static uint8_t deckSave[sizeof(emojiBuf)];  // staging so emojiBuf never tears
uint16_t *deckCache[DECK_MAX] = {nullptr};  // PSRAM copy of each slot: spin never hits flash

// ---- config (Preferences/NVS: POSIX TZ, never readable back)
Preferences prefs;
String tzStr;
// signature of the deck currently on this device, set by the host after a
// sync so a reconnect can skip re-uploading an unchanged deck
String deckSig;

// set by a TIME: push from the host; false = clock face without hands
bool timeValid = false;

// ---- standalone clock / roulette / toast
float prevHA, prevMA, prevSA;               // last-drawn hand angles (erase)
bool prevHandsValid = false;
unsigned long toastUntil = 0;
unsigned long rouletteStart = 0, rouletteNext = 0;
float rouletteIval = 60.0f;
uint8_t rouletteWinner = 0, rouletteIdx = 0;

// ---------------------------------------------------------------- brightness (AMOLED: panel command, no backlight pin)
// present pushes the framebuffer to the panel; every painting routine ends
// with it, so nothing is drawn without being shown.
inline void present() { gfx->flush(); }

void brightness(uint8_t pct) {         // 0-100 (a panel command, not a canvas one)
  panel->setBrightness((uint32_t)pct * 255 / 100);
}

// elapsed reports whether a millis() deadline has passed, correctly across
// the 49.7-day wrap (a bare `millis() > deadline` reopens the boot pairing
// window when the counter returns to 0).
bool elapsed(unsigned long deadline) { return (long)(millis() - deadline) >= 0; }

// ---------------------------------------------------------------- helpers
void ringSolid(int16_t r, int16_t w, uint16_t color) {
  gfx->fillArc(CENTER, CENTER, r, r - w, 0, 360, color);
}

// cy = vertical center of the rendered text (GFX free fonts draw from baseline)
void textCenteredS(const char *s, int16_t cy, const GFXfont *font, uint8_t scale, uint16_t color) {
  gfx->setFont(font);
  gfx->setTextSize(scale);
  gfx->setTextColor(color);
  int16_t x1, y1; uint16_t tw, th;
  gfx->getTextBounds(s, 0, 0, &x1, &y1, &tw, &th);
  gfx->setCursor(CENTER - tw / 2 - x1, cy - th / 2 - y1);
  gfx->print(s);
  gfx->setTextSize(1);
}

void textCentered(const char *s, int16_t cy, const GFXfont *font, uint16_t color) {
  textCenteredS(s, cy, font, 2, color);   // 2x pixel-doubled fonts at 466px
}

// ---- icons (spec 24x24 grid, scale s ~3.9 -> ~90px, centered at cx,cy)
void iconMic(int cx, int cy, uint16_t body, float s = 3.9f) {
  int x0 = cx - 12 * s, y0 = cy - 12 * s;
  gfx->fillRoundRect(x0 + 9 * s, y0 + 3 * s, 6 * s, 11 * s, 3 * s, body);      // capsule
  gfx->fillArc(cx, y0 + 11 * s, 6 * s, 6 * s - 4, 0, 180, body);               // cradle arc
  gfx->fillRect(cx - 3, y0 + 17 * s, 6, 4 * s, body);                          // stem
}

void iconShare(int cx, int cy, uint16_t color, float s = 3.7f) {
  int x0 = cx - 12 * s, y0 = cy - 12 * s;
  for (int t = 0; t < 4; t++)                                                  // monitor, 4px stroke
    gfx->drawRoundRect(x0 + 2 * s + t, y0 + 4 * s + t, 20 * s - 2 * t, 13 * s - 2 * t, 3, color);
  for (int t = -2; t <= 2; t++) {                                              // up arrow
    gfx->drawLine(cx + t, y0 + 13 * s, cx + t, y0 + 9 * s, color);
    gfx->drawLine(cx, y0 + 9 * s, cx - 2.5f * s, y0 + 11.5f * s, color);
    gfx->drawLine(cx, y0 + 9 * s, cx + 2.5f * s, y0 + 11.5f * s, color);
  }
  gfx->fillRect(x0 + 8 * s, y0 + 20 * s, 8 * s, 4, color);                     // base
}

// ---------------------------------------------------------------- state renderers
void drawAvailable() {
  gfx->fillScreen(C_GREEN);                              // full-screen green
  ringSolid(RING_R, 8, C_WHITE);
  gfx->fillCircle(CENTER, 179, 21, C_WHITE);             // presence dot above text
  textCentered("Available", 264, &FreeSansBold18pt7b, C_WHITE);
  brightness(100);
  present();
}

void drawMeeting() {
  gfx->fillScreen(C_RED_BUSY);
  ringSolid(RING_R, 14, C_WHITE);
  iconMic(CENTER, 155, C_WHITE);
  textCentered("In a call", 283, &FreeSansBold18pt7b, C_WHITE);
  brightness(100);
  present();
}

void drawSharing() {
  gfx->fillScreen(C_PURPLE);
  ringSolid(RING_R, 16, C_WHITE);
  iconShare(CENTER, 144, C_WHITE);
  textCentered("Presenting", 260, &FreeSansBold18pt7b, C_WHITE);
  textCentered("Do not disturb", 318, &FreeSansBold9pt7b, C_LAVENDER);
  brightness(100);
  present();
}

// minimal base64 decoder (standard alphabet); returns bytes written.
// Decodes from `from` rather than taking a substring: these lines are ~38KB
// and a copy just to skip the header doubles the peak heap.
int b64decode(const String &in, uint8_t *out, int maxOut, unsigned int from = 0) {
  int n = 0, buf = 0, bits = 0;
  for (unsigned int i = from; i < in.length(); i++) {
    char c = in[i];
    int v = -1;
    if (c >= 'A' && c <= 'Z') v = c - 'A';
    else if (c >= 'a' && c <= 'z') v = c - 'a' + 26;
    else if (c >= '0' && c <= '9') v = c - '0' + 52;
    else if (c == '+') v = 62;
    else if (c == '/') v = 63;
    else if (c == '=') break;
    else continue;
    buf = (buf << 6) | v;
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      if (n < maxOut) out[n++] = (buf >> bits) & 0xFF;
    }
  }
  return n;
}

void drawEmoji() {
  if (!emojiValid) {
    gfx->fillScreen(C_BG_IDLE);
    textCentered("?", 252, &FreeSansBold18pt7b, C_GRAY_TEXT);
    brightness(70);
    present();
    return;
  }
  // 4x pixel-quadrupled: 120x120 -> 480x480, centered (7px cropped per edge,
  // invisible on the round panel). Written a column at a time, not a row:
  // the canvas rotates as it stores, so drawing coords (x, 0..465) land on
  // consecutive framebuffer words, where a row would stride 932 bytes a
  // pixel and miss the cache on every one of 217k writes.
  static uint16_t col[SCREEN_W];
  for (int x = 0; x < SCREEN_W; x++) {
    const int sx = (x + 7) >> 2;
    for (int y = 0; y < SCREEN_W; y++) col[y] = emojiBuf[((y + 7) >> 2) * 120 + sx];
    gfx->draw16bitRGBBitmap(x, 0, col, 1, SCREEN_W);
  }
  brightness(80);
  present();
}

uint16_t textW(const char *s, const GFXfont *f, uint8_t scale) {
  int16_t x1, y1; uint16_t w, h;
  gfx->setFont(f);
  gfx->setTextSize(scale);
  gfx->getTextBounds(s, 0, 0, &x1, &y1, &w, &h);
  gfx->setTextSize(1);
  return w;
}

uint16_t textH(const char *s, const GFXfont *f, uint8_t scale) {
  int16_t x1, y1; uint16_t w, h;
  gfx->setFont(f);
  gfx->setTextSize(scale);
  gfx->getTextBounds(s, 0, 0, &x1, &y1, &w, &h);
  gfx->setTextSize(1);
  return h;
}

// ---- custom message: yellow face, biggest font that fits the circle,
//      word-wrapped to the chord width available at each line

#define CUSTOM_RADIUS    194  // usable radius inside the ring (100 scaled)
#define CUSTOM_MAX_LINES 5
#define CUSTOM_MAX_WORDS 24

// horizontal space available to a text band [yTop, yBot]
uint16_t chordW(float yTop, float yBot) {
  float d = max(max(yTop - CENTER, CENTER - yTop), max(yBot - CENTER, CENTER - yBot));
  if (d >= CUSTOM_RADIUS) return 0;
  return (uint16_t)(2 * sqrtf((float)CUSTOM_RADIUS * CUSTOM_RADIUS - d * d));
}

// wrap words into at most n vertically-centered lines; false if they don't fit
bool customLayout(String *words, int wc, const GFXfont *f, uint8_t scale, float lineH, int n, String *out) {
  float top = CENTER - lineH * n / 2;
  int wi = 0;
  for (int i = 0; i < n && wi < wc; i++) {
    uint16_t maxW = chordW(top + lineH * i, top + lineH * (i + 1));
    String line = "";
    while (wi < wc) {
      String cand = line.length() ? line + " " + words[wi] : words[wi];
      if (textW(cand.c_str(), f, scale) > maxW) break;
      line = cand;
      wi++;
    }
    if (!line.length()) return false;  // a single word exceeds this line
    out[i] = line;
  }
  return wi == wc;
}

void drawCustom() {
  gfx->fillScreen(customBg);
  ringSolid(RING_R, 10, customFg);
  brightness(100);

  String words[CUSTOM_MAX_WORDS];
  int wc = 0;
  for (unsigned int i = 0; i < customText.length(); i++) {
    char ch = customText[i];
    if (ch == ' ') {
      if (words[wc].length() && wc < CUSTOM_MAX_WORDS - 1) wc++;
    } else {
      words[wc] += ch;
    }
  }
  if (words[wc].length()) wc++;
  if (!wc) { present(); return; }

  // biggest first: the 240px ladder doubled for the 466px panel
  struct { const GFXfont *f; uint8_t s; } steps[6] = {
    {&FreeSansBold24pt7b, 4}, {&FreeSansBold18pt7b, 4},
    {&FreeSansBold24pt7b, 2}, {&FreeSansBold18pt7b, 2},
    {&FreeSansBold12pt7b, 2}, {&FreeSansBold9pt7b, 2},
  };
  for (int fi = 0; fi < 6; fi++) {
    float lineH = textH("Agy", steps[fi].f, steps[fi].s) * 1.05f;
    int maxLines = min(CUSTOM_MAX_LINES, (int)(2 * CUSTOM_RADIUS / lineH));
    for (int n = 1; n <= maxLines; n++) {
      String lines[CUSTOM_MAX_LINES];
      if (!customLayout(words, wc, steps[fi].f, steps[fi].s, lineH, n, lines)) continue;
      float top = CENTER - lineH * n / 2;
      for (int i = 0; i < n; i++)
        textCenteredS(lines[i].c_str(), (int16_t)(top + lineH * (i + 0.5f)), steps[fi].f, steps[fi].s, customFg);
      present();
      return;
    }
  }
  textCenteredS(customText.c_str(), CENTER, &FreeSansBold9pt7b, 2, customFg); // best effort
  present();
}

void drawFlashing() {
  gfx->fillScreen(C_RED_BUSY);
  ringSolid(RING_R, 16, C_RED_MRING);
  textCentered("Flashing", 217, &FreeSansBold18pt7b, C_WHITE);
  textCentered("do not power off", 295, &FreeSansBold9pt7b, C_WHITE);
  brightness(100);
  present();
}

// ---- standalone analog clock (replaces the old "- -" OFF screen)

// hand from CENTER: tail px behind the pivot, len px ahead, 2*halfW+1 px wide
void handLine(float ang, int tail, int len, int halfW, uint16_t color) {
  float dx = sinf(ang), dy = -cosf(ang);   // 0 = 12 o'clock, clockwise
  float px = cosf(ang), py = sinf(ang);    // perpendicular, for thickness
  for (int i = -halfW; i <= halfW; i++)
    gfx->drawLine(CENTER + px * i - dx * tail, CENTER + py * i - dy * tail,
                  CENTER + px * i + dx * len, CENTER + py * i + dy * len, color);
}

// erase-and-redraw hands only: no full-screen repaint, no flicker.
// Hands stay inside r=190; ticks start at r=192, so erasing never chips them.
void drawClockHands() {
  time_t now = time(nullptr);
  struct tm t;
  localtime_r(&now, &t);
  float sa = t.tm_sec * 6 * DEG_TO_RAD;
  float ma = (t.tm_min + t.tm_sec / 60.0f) * 6 * DEG_TO_RAD;
  float ha = (t.tm_hour % 12 + t.tm_min / 60.0f) * 30 * DEG_TO_RAD;
  if (prevHandsValid) {
    handLine(prevSA, 30, 176, 2, C_BG_IDLE);
    handLine(prevMA, 24, 150, 4, C_BG_IDLE);
    handLine(prevHA, 24, 104, 6, C_BG_IDLE);
  }
  handLine(ha, 24, 104, 6, C_WHITE);
  handLine(ma, 24, 150, 4, C_LAVENDER);
  handLine(sa, 30, 176, 2, C_GREEN);
  gfx->fillCircle(CENTER, CENTER, 12, C_LAVENDER);       // center hub
  gfx->fillCircle(CENTER, CENTER, 5, C_BG_IDLE);
  prevHA = ha; prevMA = ma; prevSA = sa;
  prevHandsValid = true;
  present();
}

void drawClock() {
  gfx->fillScreen(C_BG_IDLE);
  for (int i = 0; i < 60; i++) {                         // minute ticks
    float a = i * 6 * DEG_TO_RAD;
    float dx = sinf(a), dy = -cosf(a);
    if (i % 5 == 0) {                                    // hour marks, 3px
      float px = cosf(a), py = sinf(a);
      for (int o = -1; o <= 1; o++)
        gfx->drawLine(CENTER + px * o + dx * 192, CENTER + py * o + dy * 192,
                      CENTER + px * o + dx * 214, CENTER + py * o + dy * 214, C_LAVENDER);
    } else {
      gfx->drawLine(CENTER + dx * 204, CENTER + dy * 204,
                    CENTER + dx * 214, CENTER + dy * 214, C_GRAY_RING);
    }
  }
  prevHandsValid = false;
  if (timeValid) {
    drawClockHands();
    brightness(35);
    return;                                              // drawClockHands presented it
  }
  brightness(10);                                        // dimmed face, no hands
  present();
}

// 1 Hz hand update while the clock is showing (polled at most every 200 ms so
// the common case never touches time()/localtime_r)
void clockTick() {
  static unsigned long lastTick = 0;
  static int lastSec = -1;
  if (millis() - lastTick < 200) return;
  lastTick = millis();
  time_t now = time(nullptr);
  struct tm t;
  localtime_r(&now, &t);
  if (t.tm_sec == lastSec && prevHandsValid) return;
  lastSec = t.tm_sec;
  drawClockHands();
}

// brief message over the clock face; face repainted when it expires
void drawToast(const char *msg) {
  gfx->fillCircle(CENTER, CENTER, 178, C_BG_IDLE);       // wipes hands too (sec hand reaches r=176)
  textCentered(msg, 233, &FreeSansBold9pt7b, C_YELLOW);
  brightness(35);
  prevHandsValid = false;
  present();
  toastUntil = millis() + 2000;
}

// ---- emoji deck (LittleFS) & roulette

const char *deckPath(uint8_t slot) {
  static char p[20];
  snprintf(p, sizeof(p), "/deck/%u.rgb", slot);
  return p;
}

// copy an image into the PSRAM cache for slot; no-op if PSRAM is unavailable
void deckCachePut(uint8_t slot, const uint8_t *data) {
  if (slot >= DECK_MAX) return;
  if (!deckCache[slot]) deckCache[slot] = (uint16_t *)ps_malloc(sizeof(emojiBuf));
  if (deckCache[slot]) memcpy(deckCache[slot], data, sizeof(emojiBuf));
}

void deckScan() {
  deckN = 0;
  for (uint8_t i = 0; i < DECK_MAX; i++) {
    File f = LittleFS.open(deckPath(i), "r");
    bool present = f && f.size() == sizeof(emojiBuf);   // skip torn writes
    if (present) {
      deckSlots[deckN++] = i;
      if (!deckCache[i]) {                              // load once into PSRAM
        uint16_t *buf = (uint16_t *)ps_malloc(sizeof(emojiBuf));
        if (buf) {
          if (f.read((uint8_t *)buf, sizeof(emojiBuf)) == sizeof(emojiBuf)) deckCache[i] = buf;
          else free(buf);                               // torn read: serve from flash
        }
      }
    } else if (deckCache[i]) {                          // slot gone: drop its cache
      free(deckCache[i]);
      deckCache[i] = nullptr;
    }
    if (f) f.close();
  }
}

bool deckLoad(uint8_t slot) {
  if (slot < DECK_MAX && deckCache[slot]) {             // cache hit: no flash I/O
    memcpy(emojiBuf, deckCache[slot], sizeof(emojiBuf));
    return true;
  }
  File f = LittleFS.open(deckPath(slot), "r");
  if (!f) return false;
  size_t n = f.read((uint8_t *)emojiBuf, sizeof(emojiBuf));
  f.close();
  return n == sizeof(emojiBuf);
}

bool deckStore(uint8_t slot, uint8_t flags) {
  File f = LittleFS.open(deckPath(slot), "w");
  bool ok = false;
  if (f) {
    ok = f.write(deckSave, sizeof(deckSave)) == sizeof(deckSave);
    f.close();
  }
  deckCachePut(slot, deckSave);                         // keep the spin cache current
  if (flags & EMO_FLAG_DECK_LAST) deckScan();           // deck sync done: refresh the index
  return ok;                                           // the ack must not claim an unwritten slot
}

void startSpin() {
  if (pairingMode || state == ST_ROULETTE_SPIN) return;
  if (deckN == 0) {
    if (state == ST_OFF) drawToast("no emojis synced");
    return;
  }
  rouletteWinner = deckSlots[esp_random() % deckN];      // uniform winner
  rouletteIdx = esp_random() % deckN;
  rouletteIval = 60.0f;
  rouletteStart = millis();
  rouletteNext = rouletteStart;
  state = ST_ROULETTE_SPIN;
  lastStateChg = millis();
}

void roulettePoll() {
  if (state != ST_ROULETTE_SPIN) return;
  unsigned long now = millis();
  if (!elapsed(rouletteNext)) return;
  if (now - rouletteStart >= 5000) {                     // settle on the winner
    if (deckLoad(rouletteWinner)) emojiValid = true;
    state = ST_ROULETTE_WINNER;
    lastStateChg = millis();
    redrawState();
    char ev[16];
    snprintf(ev, sizeof(ev), "ROULETTE:%u", rouletteWinner);
    emitEvent(ev);
    return;
  }
  if (deckN == 0) {                // deck emptied under us mid-spin: `% 0`
    state = ST_OFF;                // faults the core, so bail to the clock
    lastStateChg = millis();
    redrawState();
    return;
  }
  rouletteIdx = (rouletteIdx + 1) % deckN;
  if (deckLoad(deckSlots[rouletteIdx])) { emojiValid = true; redrawState(); }
  rouletteIval *= 1.09f;         // ease out: ~60ms frames -> ~0.5s over 5s
  rouletteNext = now + (unsigned long)rouletteIval;
}

// full-screen 6-digit passkey while the host's OS shows its pairing dialog
void drawPairing(uint32_t passkey) {
  char code[8];
  snprintf(code, sizeof(code), "%06lu", (unsigned long)passkey);
  gfx->fillScreen(C_BG_IDLE);
  ringSolid(RING_R, 8, C_LAVENDER);
  textCentered("Bluetooth pairing", 130, &FreeSansBold9pt7b, C_LAVENDER);
  textCenteredS(code, 233, &FreeSansBold24pt7b, 3, C_WHITE);
  textCentered("enter this code", 336, &FreeSansBold9pt7b, C_GRAY_TEXT);
  brightness(100);
  present();
}

// an overlay (pairing screen or a toast) currently owns the panel: animators
// must not paint over it. drawPairScreen()/drawToast() forward-declared below.
void drawPairScreen();
bool overlayActive() { return pairingMode || toastUntil || shownPasskey; }

// states that survive a liveness loss (BLE drop / USB watchdog) instead of
// falling back to the standalone clock: a flash-in-progress and a roulette.
bool stateSticky(State s) {
  return s == ST_FLASHING || s == ST_ROULETTE_SPIN || s == ST_ROULETTE_WINNER;
}

// the single screen-ownership chokepoint: precedence pairing > toast > state.
// every non-animator draw goes through here so no other site repaints blindly.
void redrawState() {
  if (shownPasskey) { drawPairing(shownPasskey); return; }  // passkey display owns the panel
  if (pairingMode)  { drawPairScreen(); return; }           // pairing prompt owns it
  if (toastUntil) return;                           // toast owns it until it expires
  switch (state) {
    case ST_AVAILABLE:       drawAvailable(); break;
    case ST_MEETING:         drawMeeting();   break;
    case ST_SHARING:         drawSharing();   break;
    case ST_FLASHING:        drawFlashing();  break;
    case ST_CUSTOM:          drawCustom();    break;
    case ST_EMOJI:           drawEmoji();     break;
    case ST_ROULETTE_SPIN:   drawEmoji();     break;  // current spin frame in emojiBuf
    case ST_ROULETTE_WINNER: drawEmoji();     break;  // settled winner in emojiBuf
    default:                 drawClock();     break;
  }
}

void setState(State s) {
  if (s == state) return;
  state = s;
  lastStateChg = millis();
  redrawState();
}

// ---------------------------------------------------------------- pairing gesture & mode
// progress ring fills clockwise from 12 o'clock. Arduino_GFX angles run
// clockwise from 3 o'clock, so 12 o'clock is 270 deg; wraps back past the top.
void arcClockwiseFromTop(int16_t r1, int16_t r2, int deg, uint16_t color) {
  if (deg <= 0) return;
  if (deg > 360) deg = 360;
  float end = 270.0f + deg;
  if (end <= 360.0f) {
    gfx->fillArc(CENTER, CENTER, r1, r2, 270.0f, end, color);
  } else {
    gfx->fillArc(CENTER, CENTER, r1, r2, 270.0f, 360.0f, color);
    gfx->fillArc(CENTER, CENTER, r1, r2, 0.0f, end - 360.0f, color);
  }
}

// draw/advance the hold-to-pair progress ring; lastDeg tracks the drawn angle
// so the green fill only repaints when it grows (no flicker).
void drawProgressRing(unsigned long held, int &lastDeg) {
  if (lastDeg < 0) ringSolid(PROG_R, PROG_W, C_GRAY_RING);   // gray track, once
  int deg = (int)(((long)held - (long)HOLD_RING_MS) * 360 / (long)(HOLD_PAIR_MS - HOLD_RING_MS));
  if (deg < 0) deg = 0; else if (deg > 360) deg = 360;
  // repaint in 6° steps (60 over the gesture, still reads as continuous):
  // a redraw costs a whole-framebuffer push on the AMOLED, and at every
  // poll that starved serial, BLE and touch for much of the hold
  if (deg != 360 && deg - lastDeg < 6) {
    if (lastDeg < 0) lastDeg = deg;   // the track is drawn; record it, or a
    return;                           // release here reads as a long press
  }
  if (deg == lastDeg) return;
  lastDeg = deg;
  arcClockwiseFromTop(PROG_R, PROG_R - PROG_W, deg, C_GREEN);
  present();
}

// theme-styled pairing prompt: title + advertised device name
void drawPairScreen() {
  gfx->fillScreen(C_BG_IDLE);
  ringSolid(RING_R, 8, C_GREEN);
  textCentered("Pair with onIT", 180, &FreeSansBold12pt7b, C_WHITE);
  textCentered(bleName, 250, &FreeSansBold9pt7b, C_LAVENDER);
  textCentered("hold 10s to exit", 320, &FreeSansBold9pt7b, C_GRAY_TEXT);
  brightness(100);
  present();
}

void enterPairing() {
  if (state == ST_ROULETTE_SPIN) state = ST_ROULETTE_WINNER;  // freeze an in-progress spin
  pairingMode = true;
  pairingModeUntil = millis() + PAIR_MODE_MS;
  drawPairScreen();
}

void exitPairing() {
  pairingMode = false;
  pendingPasskey = 0;
  shownPasskey = 0;
  redrawState();                                 // restore clock / winner / presence / off
}

void togglePairing() {
  if (pairingMode) exitPairing();
  else enterPairing();
}

// ---------------------------------------------------------------- events (serial + BLE notify)
void emitEvent(const char *s) {
  Serial.print(s);
  Serial.print("\n");
  if (evtChr && bleConns > 0) {
    evtChr->setValue((const uint8_t *)s, strlen(s));
    evtChr->notify();
  }
}

// ---------------------------------------------------------------- command parser (serial + BLE)
bool isHex6(const String &s, int off) {
  for (int i = 0; i < 6; i++)
    if (!isxdigit(s[off + i])) return false;
  return true;
}

// six hex chars at off -> RGB565
uint16_t hex565(const String &s, int off) {
  long v = strtol(s.substring(off, off + 6).c_str(), NULL, 16);
  return ((uint16_t)((v >> 16 & 0xFF) >> 3) << 11) |
         ((uint16_t)((v >> 8 & 0xFF) >> 2) << 5) |
         (uint16_t)((v & 0xFF) >> 3);
}

void handleLine(const String &line) {
  if (line == "VERSION") { emitEvent("VERSION:" FW_VERSION ":" BOARD_TAG); return; }
  if (line == "SPIN") { startSpin(); return; }
  // config lines (BLE Command characteristic; also accepted over serial)
  if (line.startsWith("TIME:")) {          // UTC epoch seconds from the host
    long long epoch = atoll(line.substring(5).c_str());
    if (epoch > 0) {
      struct timeval tv = { .tv_sec = (time_t)epoch, .tv_usec = 0 };
      settimeofday(&tv, nullptr);
      bool first = !timeValid;
      timeValid = true;
      // the standalone face is the only screen showing time; repaint it so
      // the hands appear on the first push and jump on a correction
      if (state == ST_OFF && !overlayActive()) {
        if (first) redrawState();
        else drawClockHands();
      }
    }
    return;
  }
  if (line.startsWith("TZ:")) {
    String tz = line.substring(3);
    if (tz != tzStr) {                     // no-op if the zone is unchanged
      tzStr = tz;
      prefs.putString("tz", tzStr);
      setenv("TZ", tzStr.c_str(), 1);
      tzset();
    }
    return;
  }
  if (line.startsWith("DECK:")) {          // deck size after sync: drop stale slots
    int n = line.substring(5).toInt();
    for (int i = max(n, 0); i < DECK_MAX; i++)
      LittleFS.remove(deckPath(i));
    deckScan();
    return;
  }
  if (line.startsWith("DECKIMG:")) {       // DECKIMG:<slot>:<last>:<base64>
    // deck upload over serial (BLE uses the binary Emoji characteristic).
    // Acked per image with DECKOK so the host paces the next one — a whole
    // deck at 115200 outruns both the RX buffer and the LittleFS write.
    int c1 = line.indexOf(':', 8);
    int c2 = line.indexOf(':', c1 + 1);
    if (c1 < 0 || c2 < 0) return;
    int slot = line.substring(8, c1).toInt();
    if (slot < 0 || slot >= DECK_MAX) return;
    uint8_t flags = line.substring(c1 + 1, c2).toInt() ? EMO_FLAG_DECK_LAST : 0;
    if (b64decode(line, deckSave, sizeof(deckSave), c2 + 1) != (int)sizeof(deckSave))
      return;                              // short or garbled: no ack, the host gives up on this sync
    if (!deckStore(slot, flags)) return;    // no ack: the host retries the sync
    char ev[16];
    snprintf(ev, sizeof(ev), "DECKOK:%u", (unsigned)slot);
    emitEvent(ev);
    return;
  }
  if (line.startsWith("DECKSIG:")) {       // host records what this deck is
    deckSig = line.substring(8);
    prefs.putString("decksig", deckSig);
    return;
  }
  if (line == "DECKSIG") {                 // ...and asks, to skip a resync
    // an empty deck can't match any signature, however NVS survived
    emitEvent(("DECKSIG:" + (deckN ? deckSig : String())).c_str());
    return;
  }
  if (line.startsWith("EMOJI:")) {
    lastCmd = millis();
    int n = b64decode(line, (uint8_t *)emojiBuf, sizeof(emojiBuf), 6);
    emojiValid = (n == (int)sizeof(emojiBuf));
    state = ST_EMOJI;
    lastStateChg = millis();
    redrawState();
    return;
  }
  if (!line.startsWith("STATE:")) return;
  lastCmd = millis();
  if (state == ST_ROULETTE_SPIN) return;   // host-command policy: STATE:* ignored mid-spin
  String s = line.substring(6); s.trim();
  if (s.startsWith("custom:")) {
    String msg = s.substring(7);
    uint16_t bg = C_YELLOW, fg = C_BLACK;
    // optional RRGGBB,RRGGBB: color prefix
    if (msg.length() >= 14 && msg[6] == ',' && msg[13] == ':' &&
        isHex6(msg, 0) && isHex6(msg, 7)) {
      bg = hex565(msg, 0);
      fg = hex565(msg, 7);
      msg = msg.substring(14);
    }
    if (state != ST_CUSTOM || msg != customText || bg != customBg || fg != customFg) {
      customText = msg;
      customBg = bg;
      customFg = fg;
      state = ST_CUSTOM;
      lastStateChg = millis();
      redrawState();
    }
    return;
  }
  if      (s == "available") setState(ST_AVAILABLE);
  else if (s == "meeting")   setState(ST_MEETING);
  else if (s == "sharing")   setState(ST_SHARING);
  else if (s == "flashing")  setState(ST_FLASHING);
  else if (s == "emoji") {   // host-command policy: heartbeat must not disturb a roulette winner
    if (state != ST_ROULETTE_WINNER) setState(ST_EMOJI);
  }
  else                       setState(ST_OFF);
}

// ---------------------------------------------------------------- BLE server
class ServerCB : public NimBLEServerCallbacks {
  void onConnect(NimBLEServer *pServer, NimBLEConnInfo &connInfo) override {
    lastConnHandle = connInfo.getConnHandle();
    portENTER_CRITICAL(&bleMux);
    bleConns++;
    portEXIT_CRITICAL(&bleMux);
  }

  void onDisconnect(NimBLEServer *pServer, NimBLEConnInfo &connInfo, int reason) override {
    portENTER_CRITICAL(&bleMux);
    if (bleConns > 0) bleConns--;
    emojiRxCount = 0;               // discard partial image, never tear
    emojiRxReady = false;
    portEXIT_CRITICAL(&bleMux);
    pendingPasskey = 0;            // a cancelled/abandoned pairing drops its passkey,
    shownPasskey = 0;              // including one loop() has not drawn yet
    bleDropped = true;              // loop shows OFF/STALE
  }

  // DisplayOnly: we render the passkey, the host types it
  uint32_t onPassKeyDisplay() override {
    if (elapsed(pairableUntil) && !pairingMode) { // outside boot window & not pairing: reject
      // NimBLE injects this return value as the passkey (NimBLEServer.cpp),
      // and the terminate above is async and aimed at the newest connection,
      // which need not be this peer — so never return a guessable 0 here.
      NimBLEDevice::getServer()->disconnect(lastConnHandle);
      return esp_random() % 1000000;   // never rendered: unguessable
    }
    uint32_t key = esp_random() % 1000000;
    pendingPasskey = key;           // loop draws the pairing screen
    return key;
  }

  void onAuthenticationComplete(NimBLEConnInfo &connInfo) override {
    if (!connInfo.isEncrypted())
      NimBLEDevice::getServer()->disconnect(connInfo.getConnHandle());
    pendingPasskey = 0;
    shownPasskey = 0;               // passkey no longer needed
    pairingDone = true;             // loop redraws the current state
  }
};

class CmdCB : public NimBLECharacteristicCallbacks {
  void onWrite(NimBLECharacteristic *chr, NimBLEConnInfo &connInfo) override {
    NimBLEAttValue v = chr->getValue();
    size_t n = v.size();
    while (n && (v[n - 1] == '\n' || v[n - 1] == '\r')) n--;
    if (!n || n >= CMD_MAX) return;
    portENTER_CRITICAL(&bleMux);
    if (cmdCount < CMD_Q_LEN) {
      uint8_t slot = (cmdHead + cmdCount) % CMD_Q_LEN;
      memcpy(cmdQ[slot], v.data(), n);
      cmdQ[slot][n] = '\0';
      cmdCount++;
    }
    portEXIT_CRITICAL(&bleMux);
  }
};

class EmojiCB : public NimBLECharacteristicCallbacks {
  void onWrite(NimBLECharacteristic *chr, NimBLEConnInfo &connInfo) override {
    NimBLEAttValue v = chr->getValue();
    if (v.size() < EMO_HDR_LEN + 1) return;   // header + at least one payload byte
    const uint8_t *d = v.data();
    // v2 header, EMO_HDR_LEN bytes LE: offset u32, total u16, seq u16, slot u8, flags u8
    uint32_t offset = (uint32_t)d[0] | (uint32_t)d[1] << 8 |
                      (uint32_t)d[2] << 16 | (uint32_t)d[3] << 24;
    uint16_t total = (uint16_t)d[4] | (uint16_t)d[5] << 8;  // (seq at d[6..7] unused)
    uint8_t slot = d[8], flags = d[9];
    uint32_t n = v.size() - EMO_HDR_LEN;
    // bounds: reject before offset+n can wrap u32 (a crafted offset near
    // 0xFFFFFFFF would otherwise pass and overflow the memcpy target)
    if (total != sizeof(emojiBuf) || offset > sizeof(emojiBuf) ||
        n > sizeof(emojiBuf) - offset) return;
    if (slot != EMO_SLOT_LIVE && slot >= DECK_MAX) return;
    portENTER_CRITICAL(&bleMux);
    if (offset == 0) {                         // new image restarts assembly;
      emojiRxCount = 0;                        // drop an unconsumed image too,
      emojiRxReady = false;                    // never persist a torn mix
    }
    memcpy(emojiRx + offset, d + EMO_HDR_LEN, n);
    emojiRxCount += n;
    emojiRxLast = millis();
    emojiRxSlot = slot;
    emojiRxFlags = flags;
    if (emojiRxCount >= sizeof(emojiBuf)) emojiRxReady = true;
    portEXIT_CRITICAL(&bleMux);
  }
};

ServerCB serverCB;
CmdCB cmdCB;
EmojiCB emojiCB;

void bleInit() {
  uint8_t mac[6];
  esp_read_mac(mac, ESP_MAC_BT);
  snprintf(bleName, sizeof(bleName), "onIT-AMOLED-%02X%02X", mac[4], mac[5]);

  NimBLEDevice::init(bleName);
  NimBLEDevice::setMTU(517);                    // 512 B emoji chunks in one write
  NimBLEDevice::setSecurityAuth(true, true, true);   // bond + MITM + LE Secure Connections
  NimBLEDevice::setSecurityIOCap(BLE_HS_IO_DISPLAY_ONLY);

  NimBLEServer *server = NimBLEDevice::createServer();
  server->setCallbacks(&serverCB);
  server->advertiseOnDisconnect(true);

  NimBLEService *svc = server->createService(BLE_UUID_SVC);
  NimBLECharacteristic *cmd = svc->createCharacteristic(
      BLE_UUID_CMD,
      NIMBLE_PROPERTY::WRITE | NIMBLE_PROPERTY::WRITE_ENC | NIMBLE_PROPERTY::WRITE_AUTHEN);
  cmd->setCallbacks(&cmdCB);
  NimBLECharacteristic *emo = svc->createCharacteristic(
      BLE_UUID_EMO,
      NIMBLE_PROPERTY::WRITE | NIMBLE_PROPERTY::WRITE_ENC | NIMBLE_PROPERTY::WRITE_AUTHEN);
  emo->setCallbacks(&emojiCB);
  evtChr = svc->createCharacteristic(
      BLE_UUID_EVT,
      NIMBLE_PROPERTY::READ | NIMBLE_PROPERTY::READ_ENC | NIMBLE_PROPERTY::READ_AUTHEN |
      NIMBLE_PROPERTY::NOTIFY);
  evtChr->setValue("VERSION:" FW_VERSION ":" BOARD_TAG);  // encrypted read triggers pairing
  svc->start();

  NimBLEAdvertising *adv = NimBLEDevice::getAdvertising();
  adv->addServiceUUID(BLE_UUID_SVC);
  adv->setName(bleName);
  adv->enableScanResponse(true);
  adv->start();
}

// ---------------------------------------------------------------- touch
void touchInit() {
  touch.setPins(TP_RST, TP_INT);
  touchOk = touch.begin(Wire, CST92XX_SLAVE_ADDRESS, TP_SDA, TP_SCL);
}

// poll for contact. A release under HOLD_LONG_MS is a TAP, a longer release
// (before the ring) a LONG. From HOLD_RING_MS a green progress ring fills toward
// HOLD_PAIR_MS; releasing while it shows cancels the gesture (no TAP/LONG) and
// restores the screen. A full HOLD_PAIR_MS hold toggles BLE pairing mode.
void touchPoll() {
  static unsigned long lastPoll = 0;
  static bool wasDown = false;
  static unsigned long downAt = 0;
  static int ringDeg = -1;             // >=0 while the progress ring is showing
  static bool consumed = false;        // HOLD_PAIR_MS fired: swallow the rest of the touch
  if (!touchOk || millis() - lastPoll < 50) return;
  lastPoll = millis();
  bool down = touch.getTouchPoints().getPointCount() > 0;

  if (down && !wasDown) {                        // finger down: start timing
    wasDown = true;
    downAt = millis();
    ringDeg = -1;
    consumed = false;
  } else if (down && !consumed) {                // finger held
    unsigned long held = millis() - downAt;
    if (held >= HOLD_PAIR_MS) {                  // 10s: enter/leave pairing mode
      consumed = true;
      ringDeg = -1;
      togglePairing();
    } else if (held >= HOLD_RING_MS) {           // 2s+: grow the progress ring
      drawProgressRing(held, ringDeg);
    }
  } else if (!down && wasDown) {                 // release
    wasDown = false;
    unsigned long held = millis() - downAt;
    if (consumed) {                              // pairing toggle already handled
      // nothing
    } else if (ringDeg >= 0) {                   // ring shown then released early: cancel
      redrawState();
    } else if (pairingMode) {                    // taps do nothing while pairing
      // nothing
    } else if (held >= HOLD_LONG_MS) {
      emitEvent("TOUCH:LONG");
    } else {
      emitEvent("TOUCH:TAP");
      // standalone (no live host): a tap on the clock or winner spins the wheel
      bool hostLive = bleConns > 0 || (lastCmd && millis() - lastCmd < 5000);
      if (!hostLive && (state == ST_OFF || state == ST_ROULETTE_WINNER)) startSpin();
    }
  }
}

// ---------------------------------------------------------------- setup/loop
void setup() {
  // 38KB EMOJI:/DECKIMG: lines arrive at USB speed on a native-USB board,
  // so give the ring room to absorb a burst while a repaint holds the loop.
  Serial.setRxBufferSize(16384);
  Serial.begin(115200);
  Wire.begin(TP_SDA, TP_SCL);
  if (!gfx->begin()) {
    // the 434KB framebuffer comes from PSRAM; without it every primitive
    // dereferences null and the board boot-loops with nothing on screen
    panel->begin();
    panel->fillScreen(C_BLACK);
    panel->setFont(&FreeSansBold12pt7b);
    panel->setTextColor(C_RED_MRING);
    panel->setCursor(60, CENTER);
    panel->print("no PSRAM");
    panel->setBrightness(255);
    while (true) delay(1000);
  }
  touchInit();
  prefs.begin("onit", false);
  tzStr = prefs.getString("tz", "");
  deckSig = prefs.getString("decksig", "");
  if (tzStr.length()) { setenv("TZ", tzStr.c_str(), 1); tzset(); }
  LittleFS.begin(true);           // mounts the default "spiffs" data partition
  LittleFS.mkdir("/deck");
  deckScan();
  drawClock();
  bleInit();
  lastCmd = 0;
  Serial.print("VERSION:" FW_VERSION ":" BOARD_TAG "\n");   // boot banner; host resets us on connect
}

void loop() {
  // serial in
  while (Serial.available()) {
    char c = Serial.read();
    if (c == '\n') { handleLine(lineBuf); lineBuf = ""; }
    else if (c != '\r') lineBuf += c;
  }

  // BLE commands, queued by the NimBLE task
  while (cmdCount > 0) {
    char buf[CMD_MAX];
    portENTER_CRITICAL(&bleMux);
    strcpy(buf, cmdQ[cmdHead]);
    cmdHead = (cmdHead + 1) % CMD_Q_LEN;
    cmdCount--;
    portEXIT_CRITICAL(&bleMux);
    handleLine(String(buf));
  }

  // completed BLE emoji: slot 0xFF -> show it; slot 0..19 -> store in the deck
  if (emojiRxReady) {
    uint8_t slot, flags;
    portENTER_CRITICAL(&bleMux);
    slot = emojiRxSlot;
    flags = emojiRxFlags;
    memcpy(slot == EMO_SLOT_LIVE ? (uint8_t *)emojiBuf : deckSave, emojiRx, sizeof(emojiBuf));
    emojiRxReady = false;
    emojiRxCount = 0;
    portEXIT_CRITICAL(&bleMux);
    if (slot == EMO_SLOT_LIVE) {
      emojiValid = true;
      lastCmd = millis();
      state = ST_EMOJI;
      lastStateChg = millis();
      redrawState();
    } else {
      // per-image ack: the host holds the next slot's chunks until this
      // arrives, so a slow LittleFS write can never tear the rx buffer.
      // Withheld when the write failed, so the host doesn't record a slot
      // the device hasn't got.
      if (deckStore(slot, flags)) {
        char ev[16];
        snprintf(ev, sizeof(ev), "DECKOK:%u", slot);
        emitEvent(ev);
      }
    }
  }

  // stalled BLE emoji: discard after 2s without a chunk
  if (emojiRxCount > 0 && !emojiRxReady && millis() - emojiRxLast > 2000) {
    portENTER_CRITICAL(&bleMux);
    emojiRxCount = 0;
    portEXIT_CRITICAL(&bleMux);
  }

  // pairing screen lifecycle (flags set by NimBLE callbacks)
  if (pendingPasskey) {
    shownPasskey = pendingPasskey;   // persists so any repaint restores the passkey
    pendingPasskey = 0;
    drawPairing(shownPasskey);
  }
  if (pairingDone) {
    pairingDone = false;
    if (pairingMode) exitPairing();   // successful pairing leaves pairing mode
    else redrawState();
  }
  if (pairingMode && elapsed(pairingModeUntil)) exitPairing();   // 2-minute timeout

  touchPoll();
  roulettePoll();

  // 1 Hz clock hands while standalone (paused under a toast or the pairing screen)
  if (state == ST_OFF && timeValid && !overlayActive()) clockTick();

  // expired toast: repaint the clock face
  if (toastUntil && elapsed(toastUntil)) {
    toastUntil = 0;
    redrawState();   // unconditional: redrawState no-ops under a toast, so a
  }                  // state set during one has not been painted yet

  // liveness: BLE disconnect -> OFF/STALE (clock); on USB the 5s text watchdog.
  // A roulette winner persists standalone until the next spin or a host state.
  if (bleDropped) {
    bleDropped = false;
    if (!stateSticky(state)) {
      state = ST_OFF;
      lastStateChg = millis();
      redrawState();   // not setState: the screen needs restoring even when
    }                  // the state is unchanged (a cancelled pairing screen)
  }
  if (bleConns == 0 && state != ST_OFF && !stateSticky(state) &&
      millis() - lastCmd > 5000) setState(ST_OFF);

  // presenting ring pulse: 8-step LUT, 1.5s period, ring redraw only
  if (state == ST_SHARING && !overlayActive()) {
    static int lastStep = -1;
    int step = (millis() % 1500) / 187;                  // 1500/8
    if (step != lastStep) { lastStep = step; ringSolid(RING_R, 16, PULSE_LUT[step]); present(); }
  }

  // flashing ring pulse: faster, red, 1s period
  if (state == ST_FLASHING && !overlayActive()) {
    static int lastF = -1;
    int step = (millis() % 1000) / 125;                  // 1000/8
    if (step != lastF) { lastF = step; ringSolid(RING_R, 16, FLASH_LUT[step]); present(); }
  }

  delay(10);
}
