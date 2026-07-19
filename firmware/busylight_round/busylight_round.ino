/*
 * Teams Busylight — Theme 1e "TEAMS"
 * Waveshare ESP32-S3-Touch-LCD-1.28 (GC9A01 240x240 round, CST816S touch)
 *
 * Library: "GFX Library for Arduino" (moononournation Arduino_GFX)
 * Board:   ESP32S3 Dev Module (USB CDC not needed - CH343P bridges UART0)
 *
 * Serial in : STATE:available|meeting|sharing|flashing|off   @115200
 *             STATE:custom:<text>       (yellow screen, text auto-fitted)
 *             EMOJI:<base64>            (100x100 RGB565 LE image; shown
 *             immediately and kept alive by STATE:emoji heartbeats)
 *             VERSION                    (query firmware version)
 * Serial out: VERSION:x.y.z     (at boot and on VERSION query)
 * Watchdog  : no serial for 5s -> OFF/STALE (except FLASHING: sticky,
 *             shown until the flash reset - the port is closed during esptool)
 *
 * NOTE ON PINS: values below match the Waveshare wiki demo for this board.
 * If the panel stays black, verify LCD_RST/TP pins against
 * waveshare.com/wiki/ESP32-S3-Touch-LCD-1.28 for your revision.
 */

#define FW_VERSION "1.4.0"   // extracted by `make firmware`, embedded in onIT

#include <Arduino_GFX_Library.h>
#include <Adafruit_GFX.h>   // only for its Fonts/ include path
#include <Fonts/FreeSansBold18pt7b.h>
#include <Fonts/FreeSansBold12pt7b.h>
#include <Fonts/FreeSansBold9pt7b.h>

// ---------------------------------------------------------------- pins
#define LCD_SCK   10
#define LCD_MOSI  11
#define LCD_MISO  12
#define LCD_DC     8
#define LCD_CS     9
#define LCD_RST   14
#define LCD_BL     2


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
Arduino_DataBus *bus = new Arduino_ESP32SPI(LCD_DC, LCD_CS, LCD_SCK, LCD_MOSI, LCD_MISO);
Arduino_GFX *gfx = new Arduino_GC9A01(bus, LCD_RST, 0 /*rotation*/, true /*IPS*/);

enum State { ST_OFF, ST_AVAILABLE, ST_MEETING, ST_SHARING, ST_FLASHING, ST_CUSTOM, ST_EMOJI };
State state = ST_OFF;

unsigned long lastSerial   = 0;
unsigned long lastStateChg = 0;
String customText;
uint16_t emojiBuf[100 * 100];
bool emojiValid = false;
String lineBuf;

// ---------------------------------------------------------------- backlight
void backlight(uint8_t pct) {          // 0-100
  ledcWrite(LCD_BL, (uint32_t)pct * 255 / 100);
}

// ---------------------------------------------------------------- helpers
void ringSolid(int16_t r, int16_t w, uint16_t color) {
  gfx->fillArc(120, 120, r, r - w, 0, 360, color);
}

// dashed ring: nSeg segments of onDeg, gap fills the rest of the pitch
void ringDashed(int16_t r, int16_t w, uint16_t color, int nSeg, float onDeg) {
  float pitch = 360.0f / nSeg;
  for (int i = 0; i < nSeg; i++) {
    float a0 = i * pitch;
    gfx->fillArc(120, 120, r, r - w, a0, a0 + onDeg, color);
  }
}

// cy = vertical center of the rendered text (GFX free fonts draw from baseline)
void textCentered(const char *s, int16_t cy, const GFXfont *font, uint16_t color) {
  gfx->setFont(font);
  gfx->setTextSize(1);
  gfx->setTextColor(color);
  int16_t x1, y1; uint16_t tw, th;
  gfx->getTextBounds(s, 0, 0, &x1, &y1, &tw, &th);
  gfx->setCursor(120 - tw / 2 - x1, cy - th / 2 - y1);
  gfx->print(s);
}

// ---- icons (spec 24x24 grid, scale s=2 -> ~46-48px, centered at cx,cy)
void iconMic(int cx, int cy, uint16_t body, float s = 2.0f) {
  int x0 = cx - 12 * s, y0 = cy - 12 * s;
  gfx->fillRoundRect(x0 + 9 * s, y0 + 3 * s, 6 * s, 11 * s, 3 * s, body);      // capsule
  gfx->drawArc(cx, y0 + 11 * s, 6 * s, 6 * s - 2, 0, 180, body);               // cradle arc
  gfx->drawArc(cx, y0 + 11 * s, 6 * s - 1, 6 * s - 2, 0, 180, body);
  gfx->fillRect(cx - 1, y0 + 17 * s, 3, 4 * s, body);                          // stem
}

void iconShare(int cx, int cy, uint16_t color, float s = 1.9f) {
  int x0 = cx - 12 * s, y0 = cy - 12 * s;
  for (int t = 0; t < 2; t++)                                                  // monitor, 2px stroke
    gfx->drawRoundRect(x0 + 2 * s + t, y0 + 4 * s + t, 20 * s - 2 * t, 13 * s - 2 * t, 2, color);
  for (int t = -1; t <= 1; t++) {                                              // up arrow
    gfx->drawLine(cx + t, y0 + 13 * s, cx + t, y0 + 9 * s, color);
    gfx->drawLine(cx, y0 + 9 * s, cx - 2.5f * s, y0 + 11.5f * s, color);
    gfx->drawLine(cx, y0 + 9 * s, cx + 2.5f * s, y0 + 11.5f * s, color);
  }
  gfx->fillRect(x0 + 8 * s, y0 + 20 * s, 8 * s, 2, color);                     // base
}

// ---------------------------------------------------------------- state renderers
void drawAvailable() {
  gfx->fillScreen(C_BG_IDLE);
  ringSolid(114, 4, C_GREEN);                            // thin ring, hugs the edge
  gfx->fillCircle(120, 92, 11, C_GREEN);                 // presence dot above text
  textCentered("Available", 136, &FreeSansBold18pt7b, C_WHITE);
  backlight(20);
}

void drawMeeting() {
  gfx->fillScreen(C_RED_BUSY);
  ringSolid(114, 7, C_WHITE);
  iconMic(120, 80, C_WHITE);
  textCentered("In a call", 146, &FreeSansBold18pt7b, C_WHITE);
  backlight(100);
}

void drawSharing() {
  gfx->fillScreen(C_PURPLE);
  ringSolid(114, 8, C_WHITE);
  iconShare(120, 74, C_WHITE);
  textCentered("Presenting", 134, &FreeSansBold18pt7b, C_WHITE);
  textCentered("Do not disturb", 164, &FreeSansBold9pt7b, C_LAVENDER);
  backlight(100);
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
  gfx->fillScreen(C_BG_IDLE);
  if (emojiValid) gfx->draw16bitRGBBitmap(70, 70, emojiBuf, 100, 100);
  else textCentered("?", 130, &FreeSansBold18pt7b, C_GRAY_TEXT);
  backlight(70);
}

uint16_t textW(const char *s, const GFXfont *f) {
  int16_t x1, y1; uint16_t w, h;
  gfx->setFont(f);
  gfx->setTextSize(1);
  gfx->getTextBounds(s, 0, 0, &x1, &y1, &w, &h);
  return w;
}

// custom message: yellow face, text auto-fitted (shrink, then wrap to 2 lines)
void drawCustom() {
  gfx->fillScreen(C_YELLOW);
  ringSolid(114, 5, C_BLACK);
  backlight(100);
  const GFXfont *fonts[3] = {&FreeSansBold18pt7b, &FreeSansBold12pt7b, &FreeSansBold9pt7b};
  const uint16_t maxW = 180;
  for (int i = 0; i < 3; i++) {
    if (textW(customText.c_str(), fonts[i]) <= maxW) {
      textCentered(customText.c_str(), 120, fonts[i], C_BLACK);
      return;
    }
  }
  int best = -1, mid = customText.length() / 2;
  for (int i = 0; i < (int)customText.length(); i++)
    if (customText[i] == ' ' && (best < 0 || abs(i - mid) < abs(best - mid))) best = i;
  if (best > 0) {
    String a = customText.substring(0, best), b = customText.substring(best + 1);
    for (int i = 1; i < 3; i++) {
      if (textW(a.c_str(), fonts[i]) <= maxW && textW(b.c_str(), fonts[i]) <= maxW) {
        textCentered(a.c_str(), 96, fonts[i], C_BLACK);
        textCentered(b.c_str(), 144, fonts[i], C_BLACK);
        return;
      }
    }
  }
  textCentered(customText.c_str(), 120, fonts[2], C_BLACK); // best effort
}

void drawFlashing() {
  gfx->fillScreen(C_RED_BUSY);
  ringSolid(114, 8, C_RED_MRING);
  textCentered("Flashing", 112, &FreeSansBold18pt7b, C_WHITE);
  textCentered("do not power off", 152, &FreeSansBold9pt7b, C_WHITE);
  backlight(100);
}

void drawOff() {
  gfx->fillScreen(C_BLACK);
  ringDashed(114, 3, C_GRAY_RING, 48, 3.5f);             // fine dotted ring
  textCentered("- -", 124, &FreeSansBold12pt7b, C_GRAY_TEXT);
  backlight(12);                                         // dim but visible
}

void setState(State s) {
  if (s == state) return;
  state = s;
  lastStateChg = millis();
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

// ---------------------------------------------------------------- serial
void handleLine(const String &line) {
  if (line == "VERSION") { Serial.print("VERSION:" FW_VERSION "\n"); return; }
  if (line.startsWith("EMOJI:")) {
    lastSerial = millis();
    int n = b64decode(line.substring(6), (uint8_t *)emojiBuf, sizeof(emojiBuf));
    emojiValid = (n == (int)sizeof(emojiBuf));
    state = ST_EMOJI;
    lastStateChg = millis();
    drawEmoji();
    return;
  }
  if (!line.startsWith("STATE:")) return;
  lastSerial = millis();
  String s = line.substring(6); s.trim();
  if (s.startsWith("custom:")) {
    String msg = s.substring(7);
    if (state != ST_CUSTOM || msg != customText) {  // redraw on text change too
      customText = msg;
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

// ---------------------------------------------------------------- setup/loop
void setup() {
  Serial.setRxBufferSize(4096);   // EMOJI payloads burst ~27 KB
  Serial.begin(115200);
  ledcAttach(LCD_BL, 5000, 8);
  gfx->begin();
  drawOff();
  lastSerial = 0;
  Serial.print("VERSION:" FW_VERSION "\n");   // boot banner; host resets us on connect
}

void loop() {
  // serial in
  while (Serial.available()) {
    char c = Serial.read();
    if (c == '\n') { handleLine(lineBuf); lineBuf = ""; }
    else if (c != '\r') lineBuf += c;
  }

  // 5s stale watchdog
  if (state != ST_OFF && state != ST_FLASHING && millis() - lastSerial > 5000) setState(ST_OFF);

  // presenting ring pulse: 8-step LUT, 1.5s period, ring redraw only
  if (state == ST_SHARING) {
    static int lastStep = -1;
    int step = (millis() % 1500) / 187;                  // 1500/8
    if (step != lastStep) { lastStep = step; ringSolid(114, 8, PULSE_LUT[step]); }
  }

  // flashing ring pulse: faster, red, 1s period
  if (state == ST_FLASHING) {
    static int lastF = -1;
    int step = (millis() % 1000) / 125;                  // 1000/8
    if (step != lastF) { lastF = step; ringSolid(114, 8, FLASH_LUT[step]); }
  }

  delay(10);
}
