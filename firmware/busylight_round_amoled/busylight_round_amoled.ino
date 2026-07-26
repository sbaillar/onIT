/*
 * Teams Busylight — Theme 1e "TEAMS", 1.75" AMOLED edition
 * Waveshare ESP32-S3-Touch-AMOLED-1.75 (CO5300 466x466 QSPI round, CST9217 touch)
 *
 * Libraries: "GFX Library for Arduino" (moononournation Arduino_GFX, CO5300 driver)
 *            "NimBLE-Arduino" (h2zero, 2.x API)
 *            "SensorLib" (lewisxhe, TouchDrvCST92xx)
 * Board:     ESP32S3 Dev Module, USB CDC On Boot enabled (native USB, no bridge)
 *
 * Serial in : STATE:available|meeting|sharing|flashing|off   @115200
 *             STATE:custom:<text>       (yellow screen, text auto-fitted)
 *             STATE:custom:RRGGBB,RRGGBB:<text>  (background,font colors)
 *             EMOJI:<base64>            (120x120 RGB565 LE image, pixel-
 *             quadrupled to fill the screen; shown
 *             immediately and kept alive by STATE:emoji heartbeats)
 *             VERSION                    (query firmware version)
 * Serial out: VERSION:x.y.z:amoled175  (at boot and on VERSION query)
 *             TOUCH:TAP / TOUCH:LONG   (screen tapped / long-pressed;
 *             the host decides what they mean)
 *
 * BLE       : NimBLE GATT server, one service, three characteristics, all
 *             requiring an encrypted bonded link (LE Secure Connections,
 *             DisplayOnly — 6-digit passkey shown full-screen while pairing):
 *               Command (write)  same text lines as serial (STATE:*, VERSION)
 *               Emoji   (write)  8-byte header (offset u32, total u16, seq u16,
 *                                little-endian) + raw RGB565 chunk (<=504 B);
 *                                reassembled by offset, discarded on disconnect
 *                                or 2 s inter-chunk timeout
 *               Events  (notify) TOUCH:TAP / TOUCH:LONG / VERSION:x.y.z:amoled175
 *             Advertised name onIT-AMOLED-<last4 of BT MAC>. Pairable only the
 *             first 5 minutes after boot or for 5 minutes after a long-press;
 *             pairing attempts outside the window are rejected (disconnected).
 * Watchdog  : USB only: no serial for 5s -> OFF/STALE (except FLASHING: sticky,
 *             shown until the flash reset - the port is closed during esptool).
 *             While BLE is connected the link itself is the liveness signal;
 *             BLE disconnect -> OFF/STALE (except FLASHING).
 *
 * NOTE ON PINS: values below match the Waveshare demo for this board
 * (github.com/waveshareteam/ESP32-S3-Touch-AMOLED-1.75, pin_config.h).
 * If the panel stays black, verify against
 * waveshare.com/wiki/ESP32-S3-Touch-AMOLED-1.75 for your revision.
 */

#define FW_VERSION "1.0.0"   // extracted by `make firmware`, embedded in onIT

#include <Arduino_GFX_Library.h>
#include <Adafruit_GFX.h>   // only for its Fonts/ include path
#include <Wire.h>
#include <NimBLEDevice.h>
#include <TouchDrv.hpp>     // SensorLib: CST9217 over I2C
#include <esp_mac.h>
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

// ---------------------------------------------------------------- BLE
// "6f6e4954" = "onIT", "0175" = 1.75". Host must use the same UUIDs.
#define BLE_UUID_SVC "6f6e4954-0175-4b1e-8001-000000000001"
#define BLE_UUID_CMD "6f6e4954-0175-4b1e-8001-000000000002"
#define BLE_UUID_EMO "6f6e4954-0175-4b1e-8001-000000000003"
#define BLE_UUID_EVT "6f6e4954-0175-4b1e-8001-000000000004"

#define PAIR_WINDOW_MS (5UL * 60UL * 1000UL)

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
Arduino_CO5300 *gfx = new Arduino_CO5300(
    bus, LCD_RST, 0 /*rotation*/, SCREEN_W, SCREEN_W, 6, 0, 0, 0);

TouchDrvCST92xx touch;
bool touchOk = false;

enum State { ST_OFF, ST_AVAILABLE, ST_MEETING, ST_SHARING, ST_FLASHING, ST_CUSTOM, ST_EMOJI };
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
volatile uint32_t pendingPasskey = 0;       // != 0 -> show pairing screen
volatile bool pairingDone = false;          // auth finished -> redraw state
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

// ---------------------------------------------------------------- brightness (AMOLED: panel command, no backlight pin)
void brightness(uint8_t pct) {         // 0-100
  gfx->setBrightness((uint32_t)pct * 255 / 100);
}

// ---------------------------------------------------------------- helpers
void ringSolid(int16_t r, int16_t w, uint16_t color) {
  gfx->fillArc(CENTER, CENTER, r, r - w, 0, 360, color);
}

// dashed ring: nSeg segments of onDeg, gap fills the rest of the pitch
void ringDashed(int16_t r, int16_t w, uint16_t color, int nSeg, float onDeg) {
  float pitch = 360.0f / nSeg;
  for (int i = 0; i < nSeg; i++) {
    float a0 = i * pitch;
    gfx->fillArc(CENTER, CENTER, r, r - w, a0, a0 + onDeg, color);
  }
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
}

void drawMeeting() {
  gfx->fillScreen(C_RED_BUSY);
  ringSolid(RING_R, 14, C_WHITE);
  iconMic(CENTER, 155, C_WHITE);
  textCentered("In a call", 283, &FreeSansBold18pt7b, C_WHITE);
  brightness(100);
}

void drawSharing() {
  gfx->fillScreen(C_PURPLE);
  ringSolid(RING_R, 16, C_WHITE);
  iconShare(CENTER, 144, C_WHITE);
  textCentered("Presenting", 260, &FreeSansBold18pt7b, C_WHITE);
  textCentered("Do not disturb", 318, &FreeSansBold9pt7b, C_LAVENDER);
  brightness(100);
}

// minimal base64 decoder (standard alphabet); returns bytes written
int b64decode(const String &in, uint8_t *out, int maxOut) {
  int n = 0, buf = 0, bits = 0;
  for (unsigned int i = 0; i < in.length(); i++) {
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
    return;
  }
  // 4x pixel-quadrupled: 120x120 -> 480x480, centered (7px cropped per edge,
  // invisible on the round panel)
  static uint16_t row[SCREEN_W];
  int lastSy = -1;
  for (int y = 0; y < SCREEN_W; y++) {
    int sy = (y + 7) >> 2;
    if (sy != lastSy) {
      lastSy = sy;
      const uint16_t *src = &emojiBuf[sy * 120];
      for (int x = 0; x < SCREEN_W; x++) row[x] = src[(x + 7) >> 2];
    }
    gfx->draw16bitRGBBitmap(0, y, row, SCREEN_W, 1);
  }
  brightness(80);
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
  if (!wc) return;

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
      return;
    }
  }
  textCenteredS(customText.c_str(), CENTER, &FreeSansBold9pt7b, 2, customFg); // best effort
}

void drawFlashing() {
  gfx->fillScreen(C_RED_BUSY);
  ringSolid(RING_R, 16, C_RED_MRING);
  textCentered("Flashing", 217, &FreeSansBold18pt7b, C_WHITE);
  textCentered("do not power off", 295, &FreeSansBold9pt7b, C_WHITE);
  brightness(100);
}

void drawOff() {
  gfx->fillScreen(C_BLACK);
  ringDashed(RING_R, 6, C_GRAY_RING, 48, 3.5f);          // fine dotted ring
  textCentered("- -", 241, &FreeSansBold12pt7b, C_GRAY_TEXT);
  brightness(12);                                        // dim but visible
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
}

void redrawState() {
  switch (state) {
    case ST_AVAILABLE: drawAvailable(); break;
    case ST_MEETING:   drawMeeting();   break;
    case ST_SHARING:   drawSharing();   break;
    case ST_FLASHING:  drawFlashing();  break;
    case ST_CUSTOM:    drawCustom();    break;
    case ST_EMOJI:     drawEmoji();     break;
    default:           drawOff();       break;
  }
}

void setState(State s) {
  if (s == state) return;
  state = s;
  lastStateChg = millis();
  redrawState();
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
  if (line == "VERSION") { emitEvent("VERSION:" FW_VERSION ":amoled175"); return; }
  if (line.startsWith("EMOJI:")) {
    lastCmd = millis();
    int n = b64decode(line.substring(6), (uint8_t *)emojiBuf, sizeof(emojiBuf));
    emojiValid = (n == (int)sizeof(emojiBuf));
    state = ST_EMOJI;
    lastStateChg = millis();
    drawEmoji();
    return;
  }
  if (!line.startsWith("STATE:")) return;
  lastCmd = millis();
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
      drawCustom();
    }
    return;
  }
  if      (s == "available") setState(ST_AVAILABLE);
  else if (s == "meeting")   setState(ST_MEETING);
  else if (s == "sharing")   setState(ST_SHARING);
  else if (s == "flashing")  setState(ST_FLASHING);
  else if (s == "emoji")     setState(ST_EMOJI);
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
    bleDropped = true;              // loop shows OFF/STALE
  }

  // DisplayOnly: we render the passkey, the host types it
  uint32_t onPassKeyDisplay() override {
    if (millis() > pairableUntil) { // pairing window closed: reject
      NimBLEDevice::getServer()->disconnect(lastConnHandle);
      return 0;
    }
    uint32_t key = esp_random() % 1000000;
    pendingPasskey = key;           // loop draws the pairing screen
    return key;
  }

  void onAuthenticationComplete(NimBLEConnInfo &connInfo) override {
    if (!connInfo.isEncrypted())
      NimBLEDevice::getServer()->disconnect(connInfo.getConnHandle());
    pendingPasskey = 0;
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
    if (v.size() < 9) return;
    const uint8_t *d = v.data();
    uint32_t offset = (uint32_t)d[0] | (uint32_t)d[1] << 8 |
                      (uint32_t)d[2] << 16 | (uint32_t)d[3] << 24;
    uint16_t total = (uint16_t)d[4] | (uint16_t)d[5] << 8;  // (seq at d[6..7] unused)
    uint32_t n = v.size() - 8;
    if (total != sizeof(emojiBuf) || offset + n > sizeof(emojiBuf)) return;
    portENTER_CRITICAL(&bleMux);
    if (offset == 0) emojiRxCount = 0;         // new image restarts assembly
    memcpy(emojiRx + offset, d + 8, n);
    emojiRxCount += n;
    emojiRxLast = millis();
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
  char name[24];
  snprintf(name, sizeof(name), "onIT-AMOLED-%02X%02X", mac[4], mac[5]);

  NimBLEDevice::init(name);
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
  evtChr->setValue("VERSION:" FW_VERSION ":amoled175");  // encrypted read triggers pairing
  svc->start();

  NimBLEAdvertising *adv = NimBLEDevice::getAdvertising();
  adv->addServiceUUID(BLE_UUID_SVC);
  adv->setName(name);
  adv->enableScanResponse(true);
  adv->start();
}

// ---------------------------------------------------------------- touch
void touchInit() {
  touch.setPins(TP_RST, TP_INT);
  touchOk = touch.begin(Wire, CST92XX_SLAVE_ADDRESS, TP_SDA, TP_SCL);
}

// poll for contact; a release under 600ms is a TAP, holding past it a LONG.
// A long-press also reopens the BLE pairing window.
void touchPoll() {
  static unsigned long lastPoll = 0;
  static bool wasDown = false;
  static unsigned long downAt = 0;
  static bool longSent = false;
  if (!touchOk || millis() - lastPoll < 50) return;
  lastPoll = millis();
  bool down = touch.getTouchPoints().getPointCount() > 0;
  if (down && !wasDown) {
    wasDown = true;
    downAt = millis();
    longSent = false;
  } else if (down && !longSent && millis() - downAt >= 600) {
    longSent = true;
    pairableUntil = millis() + PAIR_WINDOW_MS;   // pairing gesture
    emitEvent("TOUCH:LONG");
  } else if (!down && wasDown) {
    wasDown = false;
    if (!longSent) emitEvent("TOUCH:TAP");
  }
}

// ---------------------------------------------------------------- setup/loop
void setup() {
  Serial.setRxBufferSize(4096);   // EMOJI payloads burst ~27 KB
  Serial.begin(115200);
  Wire.begin(TP_SDA, TP_SCL);
  gfx->begin();
  touchInit();
  drawOff();
  bleInit();
  lastCmd = 0;
  Serial.print("VERSION:" FW_VERSION ":amoled175\n");   // boot banner; host resets us on connect
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

  // completed BLE emoji: copy into the display buffer and show it
  if (emojiRxReady) {
    portENTER_CRITICAL(&bleMux);
    memcpy(emojiBuf, emojiRx, sizeof(emojiBuf));
    emojiRxReady = false;
    emojiRxCount = 0;
    portEXIT_CRITICAL(&bleMux);
    emojiValid = true;
    lastCmd = millis();
    state = ST_EMOJI;
    lastStateChg = millis();
    drawEmoji();
  }

  // stalled BLE emoji: discard after 2s without a chunk
  if (emojiRxCount > 0 && !emojiRxReady && millis() - emojiRxLast > 2000) {
    portENTER_CRITICAL(&bleMux);
    emojiRxCount = 0;
    portEXIT_CRITICAL(&bleMux);
  }

  // pairing screen lifecycle (flags set by NimBLE callbacks)
  if (pendingPasskey) {
    drawPairing(pendingPasskey);
    pendingPasskey = 0;
  }
  if (pairingDone) {
    pairingDone = false;
    redrawState();
  }

  touchPoll();

  // liveness: BLE disconnect -> OFF/STALE; on USB the 5s text watchdog
  if (bleDropped) {
    bleDropped = false;
    if (state != ST_FLASHING) setState(ST_OFF);
  }
  if (bleConns == 0 && state != ST_OFF && state != ST_FLASHING &&
      millis() - lastCmd > 5000) setState(ST_OFF);

  // presenting ring pulse: 8-step LUT, 1.5s period, ring redraw only
  if (state == ST_SHARING) {
    static int lastStep = -1;
    int step = (millis() % 1500) / 187;                  // 1500/8
    if (step != lastStep) { lastStep = step; ringSolid(RING_R, 16, PULSE_LUT[step]); }
  }

  // flashing ring pulse: faster, red, 1s period
  if (state == ST_FLASHING) {
    static int lastF = -1;
    int step = (millis() % 1000) / 125;                  // 1000/8
    if (step != lastF) { lastF = step; ringSolid(RING_R, 16, FLASH_LUT[step]); }
  }

  delay(10);
}
