// Meeting capture on macOS 14.2+: a Core Audio process tap on everything the
// Mac plays (the other people on the call) plus the default microphone, joined
// in one private aggregate device so both run on the same clock. The IO proc
// mixes each down to mono and queues (mic, call) frame pairs in a ring that
// Go drains. Needs NSAudioCaptureUsageDescription + NSMicrophoneUsageDescription
// in Info.plist and the audio-input entitlement under the hardened runtime.

#import <Foundation/Foundation.h>
#import <CoreAudio/CoreAudio.h>
#import <CoreAudio/AudioHardwareTapping.h>
#import <CoreAudio/CATapDescription.h>
#include <os/lock.h>
#include <stdio.h>
#include <string.h>

#define RING_FRAMES (48000 * 30) // 30 s at 48 kHz: Go drains every 200 ms

static AudioObjectID tapID = kAudioObjectUnknown;
static AudioObjectID aggID = kAudioObjectUnknown;
static AudioDeviceIOProcID procID = NULL;
static float ring[RING_FRAMES * 2];
static unsigned long long ringHead, ringTail; // frames written / read, monotonic
static os_unfair_lock ringLock = OS_UNFAIR_LOCK_INIT;
static int dropped;

static AudioObjectID defaultDevice(AudioObjectPropertySelector sel) {
    AudioObjectPropertyAddress a = {sel, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
    AudioObjectID dev = kAudioObjectUnknown;
    UInt32 sz = sizeof(dev);
    if (AudioObjectGetPropertyData(kAudioObjectSystemObject, &a, 0, NULL, &sz, &dev) != noErr) {
        return kAudioObjectUnknown;
    }
    return dev;
}

static NSString *deviceUID(AudioObjectID dev) {
    if (dev == kAudioObjectUnknown) return nil;
    AudioObjectPropertyAddress a = {kAudioDevicePropertyDeviceUID, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
    CFStringRef uid = NULL;
    UInt32 sz = sizeof(uid);
    if (AudioObjectGetPropertyData(dev, &a, 0, NULL, &sz, &uid) != noErr || uid == NULL) return nil;
    return (__bridge_transfer NSString *)uid;
}

static float monoAt(const AudioBuffer *b, UInt32 frame) {
    UInt32 ch = b->mNumberChannels ? b->mNumberChannels : 1;
    const float *d = (const float *)b->mData;
    float s = 0;
    for (UInt32 c = 0; c < ch; c++) s += d[frame * ch + c];
    return s / ch;
}

static void teardown(void) {
    if (procID) {
        AudioDeviceStop(aggID, procID);
        AudioDeviceDestroyIOProcID(aggID, procID);
        procID = NULL;
    }
    if (aggID != kAudioObjectUnknown) {
        AudioHardwareDestroyAggregateDevice(aggID);
        aggID = kAudioObjectUnknown;
    }
    if (tapID != kAudioObjectUnknown) {
        if (@available(macOS 14.2, *)) AudioHardwareDestroyProcessTap(tapID);
        tapID = kAudioObjectUnknown;
    }
}

static int fail(char *err, int errlen, const char *what, OSStatus st) {
    snprintf(err, errlen, "%s failed (OSStatus %d)", what, (int)st);
    teardown();
    return -1;
}

static int startCapture(double *rate, char *err, int errlen) API_AVAILABLE(macos(14.2));

// onitRecStart starts capture; returns 0 and the sample rate, or -1 and err.
int onitRecStart(double *rate, char *err, int errlen) {
    if (@available(macOS 14.2, *)) {
        @autoreleasepool {
            return startCapture(rate, err, errlen);
        }
    }
    snprintf(err, errlen, "meeting recording needs macOS 14.2 or later");
    return -1;
}

static int startCapture(double *rate, char *err, int errlen) {
    {
        teardown();
        os_unfair_lock_lock(&ringLock);
        ringHead = ringTail = 0;
        dropped = 0;
        os_unfair_lock_unlock(&ringLock);

        // Everything the Mac plays, excluding nothing; private so it does not
        // show up for other apps, unmuted so you still hear the call.
        CATapDescription *desc = [[CATapDescription alloc] initStereoGlobalTapButExcludeProcesses:@[]];
        desc.name = @"onIT meeting recorder";
        desc.privateTap = YES;
        desc.muteBehavior = CATapUnmuted;
        OSStatus st = AudioHardwareCreateProcessTap(desc, &tapID);
        if (st != noErr) return fail(err, errlen, "audio capture (permission?)", st);

        NSString *micUID = deviceUID(defaultDevice(kAudioHardwarePropertyDefaultInputDevice));
        NSString *outUID = deviceUID(defaultDevice(kAudioHardwarePropertyDefaultSystemOutputDevice));
        NSMutableArray *subs = [NSMutableArray array];
        if (micUID) [subs addObject:@{@kAudioSubDeviceUIDKey : micUID}];
        NSString *mainUID = micUID ?: outUID;
        if (!mainUID) return fail(err, errlen, "finding an audio device", -1);
        NSDictionary *agg = @{
            @kAudioAggregateDeviceNameKey : @"onIT recorder",
            @kAudioAggregateDeviceUIDKey : [NSUUID UUID].UUIDString,
            @kAudioAggregateDeviceMainSubDeviceKey : mainUID,
            @kAudioAggregateDeviceIsPrivateKey : @YES,
            @kAudioAggregateDeviceIsStackedKey : @NO,
            @kAudioAggregateDeviceTapAutoStartKey : @YES,
            @kAudioAggregateDeviceSubDeviceListKey : subs,
            @kAudioAggregateDeviceTapListKey : @[ @{
                @kAudioSubTapUIDKey : desc.UUID.UUIDString,
                @kAudioSubTapDriftCompensationKey : @YES,
            } ],
        };
        st = AudioHardwareCreateAggregateDevice((__bridge CFDictionaryRef)agg, &aggID);
        if (st != noErr) return fail(err, errlen, "creating the recording device", st);

        Float64 sr = 0;
        UInt32 sz = sizeof(sr);
        AudioObjectPropertyAddress ra = {kAudioDevicePropertyNominalSampleRate, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
        st = AudioObjectGetPropertyData(aggID, &ra, 0, NULL, &sz, &sr);
        if (st != noErr || sr <= 0) return fail(err, errlen, "reading the sample rate", st);

        BOOL haveMic = micUID != nil;
        st = AudioDeviceCreateIOProcIDWithBlock(&procID, aggID, NULL,
            ^(const AudioTimeStamp *now, const AudioBufferList *in, const AudioTimeStamp *inTime,
              AudioBufferList *out, const AudioTimeStamp *outTime) {
                UInt32 n = in->mNumberBuffers;
                if (n == 0) return;
                // The aggregate lists the sub-devices' input streams first and
                // the tap last: the last buffer is the call, the rest the mic.
                const AudioBuffer *call = &in->mBuffers[n - 1];
                UInt32 cch = call->mNumberChannels ? call->mNumberChannels : 1;
                UInt32 frames = call->mDataByteSize / (sizeof(float) * cch);
                os_unfair_lock_lock(&ringLock);
                for (UInt32 f = 0; f < frames; f++) {
                    if (ringHead - ringTail >= RING_FRAMES) {
                        dropped++;
                        break;
                    }
                    float mic = 0;
                    int micBufs = 0;
                    for (UInt32 b = 0; haveMic && b + 1 < n; b++) {
                        const AudioBuffer *m = &in->mBuffers[b];
                        UInt32 mch = m->mNumberChannels ? m->mNumberChannels : 1;
                        if (f < m->mDataByteSize / (sizeof(float) * mch)) {
                            mic += monoAt(m, f);
                            micBufs++;
                        }
                    }
                    unsigned long long i = ringHead % RING_FRAMES;
                    ring[2 * i] = micBufs ? mic / micBufs : 0;
                    ring[2 * i + 1] = monoAt(call, f);
                    ringHead++;
                }
                os_unfair_lock_unlock(&ringLock);
            });
        if (st != noErr) return fail(err, errlen, "creating the capture callback", st);
        st = AudioDeviceStart(aggID, procID);
        if (st != noErr) return fail(err, errlen, "starting capture", st);
        *rate = sr;
        return 0;
    }
}

// onitRecRead copies up to max queued frames (interleaved mic, call) into dst.
int onitRecRead(float *dst, int max) {
    os_unfair_lock_lock(&ringLock);
    int n = 0;
    while (n < max && ringTail < ringHead) {
        unsigned long long i = ringTail % RING_FRAMES;
        dst[2 * n] = ring[2 * i];
        dst[2 * n + 1] = ring[2 * i + 1];
        ringTail++;
        n++;
    }
    os_unfair_lock_unlock(&ringLock);
    return n;
}

void onitRecStop(void) { teardown(); }

int onitRecDropped(void) {
    os_unfair_lock_lock(&ringLock);
    int d = dropped;
    os_unfair_lock_unlock(&ringLock);
    return d;
}
