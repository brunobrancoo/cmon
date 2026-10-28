// The menu bar dot and the window it toggles (click or F3): one row per
// session and the usage limits below.
// Go owns the data and pushes snapshots through cmon_update; this file only draws.
#import <Carbon/Carbon.h>
#import <Cocoa/Cocoa.h>
#include "ui.h"

static const CGFloat kWidth = 264, kPad = 10, kRow = 22, kUsageRow = 20;
static const NSUInteger kNameMax = 10; // longer names are cut, shown whole on hover

static NSString *formatMinutes(double minutes) {
    long m = minutes < 0 ? 0 : (long)minutes;
    if (m < 1) return @"now";
    if (m < 60) return [NSString stringWithFormat:@"%ldm", m];
    if (m < 1440) return [NSString stringWithFormat:@"%ldh%02ld", m / 60, m % 60];
    return [NSString stringWithFormat:@"%ldd%ldh", m / 1440, (m % 1440) / 60];
}

static NSColor *hex(unsigned rgb) {
    return [NSColor colorWithSRGBRed:((rgb >> 16) & 0xff) / 255.0
                               green:((rgb >> 8) & 0xff) / 255.0
                                blue:(rgb & 0xff) / 255.0
                               alpha:1];
}

static NSColor *stateColor(NSString *state) {
    if ([state isEqualToString:@"working"]) return hex(0xFB923C);
    // permission and question: Claude is blocked until you answer
    if ([state isEqualToString:@"permission"] || [state isEqualToString:@"question"]) return hex(0xF87171);
    return hex(0x34D399); // done
}

static NSColor *usageColor(double percent) {
    if (percent >= 95) return hex(0xF87171);
    if (percent >= 80) return hex(0xFB923C);
    return hex(0x94A3B8);
}

@interface MonitorView : NSView
@property(strong) NSArray<NSDictionary *> *sessions;
@property(strong) NSArray<NSDictionary *> *usage; // nil: unavailable
@property NSInteger hoveredRow; // -1: none
@end

@implementation MonitorView

+ (CGFloat)heightForSessions:(NSUInteger)sessions usage:(NSUInteger)usage {
    return kPad + MAX(sessions, 1) * kRow + 9 + MAX(usage, 1) * kUsageRow + kPad - 4;
}

- (BOOL)isFlipped { return YES; }

- (NSMenu *)menuForEvent:(NSEvent *)event {
    NSMenu *menu = [[NSMenu alloc] init];
    [menu addItemWithTitle:@"Quit cmon" action:@selector(terminate:) keyEquivalent:@""];
    return menu;
}

- (void)viewDidMoveToWindow { [self updateTrackingAreas]; }

- (void)updateTrackingAreas {
    [super updateTrackingAreas];
    for (NSTrackingArea *a in self.trackingAreas) [self removeTrackingArea:a];
    NSTrackingAreaOptions options = NSTrackingMouseMoved | NSTrackingMouseEnteredAndExited |
                                    NSTrackingActiveAlways | NSTrackingInVisibleRect;
    [self addTrackingArea:[[NSTrackingArea alloc] initWithRect:NSZeroRect options:options owner:self userInfo:nil]];
}

- (void)setHover:(NSInteger)row {
    if (row == self.hoveredRow) return;
    self.hoveredRow = row;
    self.needsDisplay = YES;
}

- (void)mouseMoved:(NSEvent *)event {
    CGFloat y = [self convertPoint:event.locationInWindow fromView:nil].y - kPad;
    NSInteger row = (NSInteger)floor(y / kRow);
    [self setHover:(y >= 0 && row < (NSInteger)self.sessions.count) ? row : -1];
}

- (void)mouseEntered:(NSEvent *)event { [self mouseMoved:event]; }

- (void)mouseExited:(NSEvent *)event { [self setHover:-1]; }

- (BOOL)hasWorking {
    for (NSDictionary *s in self.sessions)
        if ([s[@"state"] isEqualToString:@"working"]) return YES;
    return NO;
}

- (void)text:(NSString *)text at:(NSRect)rect color:(NSColor *)color right:(BOOL)right mono:(BOOL)mono {
    NSMutableParagraphStyle *p = [[NSMutableParagraphStyle alloc] init];
    p.lineBreakMode = NSLineBreakByTruncatingTail;
    p.alignment = right ? NSTextAlignmentRight : NSTextAlignmentLeft;
    NSFont *font = mono ? [NSFont monospacedDigitSystemFontOfSize:11 weight:NSFontWeightRegular]
                        : [NSFont systemFontOfSize:12];
    [text drawInRect:rect withAttributes:@{
        NSFontAttributeName : font,
        NSForegroundColorAttributeName : color,
        NSParagraphStyleAttributeName : p,
    }];
}

// barFrom draws the bar between the name and the elapsed time: a segment
// sliding over the track while the session works, filled in its color otherwise.
- (void)barFrom:(CGFloat)x0 to:(CGFloat)x1 y:(CGFloat)y state:(NSString *)state now:(NSTimeInterval)now {
    CGFloat w = x1 - x0;
    if (w < 12) return;
    NSRect track = NSMakeRect(x0, y, w, 3);
    [[NSColor colorWithWhite:1 alpha:0.1] setFill];
    [[NSBezierPath bezierPathWithRoundedRect:track xRadius:1.5 yRadius:1.5] fill];
    NSColor *color = stateColor(state);
    if (![state isEqualToString:@"working"]) {
        [[color colorWithAlphaComponent:0.35] setFill];
        [[NSBezierPath bezierPathWithRoundedRect:track xRadius:1.5 yRadius:1.5] fill];
        return;
    }
    CGFloat segment = MAX(w * 0.3, 10);
    CGFloat phase = fmod(now, 1.6) / 1.6; // one sweep every 1.6s
    CGFloat start = x0 - segment + (w + segment) * phase;
    NSRect bar = NSIntersectionRect(track, NSMakeRect(start, y, segment, 3));
    [NSGraphicsContext saveGraphicsState];
    [[NSBezierPath bezierPathWithRoundedRect:track xRadius:1.5 yRadius:1.5] addClip];
    [color setFill];
    NSRectFill(bar);
    [NSGraphicsContext restoreGraphicsState];
}

- (void)drawRect:(NSRect)dirty {
    NSTimeInterval now = [[NSDate date] timeIntervalSince1970];
    NSColor *primary = [NSColor colorWithWhite:1 alpha:1];
    NSColor *secondary = [NSColor colorWithWhite:1 alpha:1];
    CGFloat inner = kWidth - 2 * kPad;
    CGFloat y = kPad;

    if (self.sessions.count == 0) {
        [self text:@"no sessions" at:NSMakeRect(kPad, y + 3, inner, 16) color:secondary right:NO mono:NO];
        y += kRow;
    }
    NSDictionary *nameAttrs = @{NSFontAttributeName : [NSFont systemFontOfSize:12]};
    NSDictionary *timeAttrs = @{NSFontAttributeName : [NSFont monospacedDigitSystemFontOfSize:11 weight:NSFontWeightRegular]};
    for (NSUInteger i = 0; i < self.sessions.count; i++) {
        NSDictionary *s = self.sessions[i];
        NSString *state = s[@"state"];
        NSColor *dot = stateColor(state);
        if ([state isEqualToString:@"working"]) {
            CGFloat pulse = 0.45 + 0.55 * fabs(sin(now * M_PI / 1.4));
            dot = [dot colorWithAlphaComponent:pulse];
        }
        [dot setFill];
        [[NSBezierPath bezierPathWithOvalInRect:NSMakeRect(kPad, y + 7, 8, 8)] fill];

        NSString *name = s[@"name"];
        if ((NSInteger)i != self.hoveredRow && name.length > kNameMax) {
            NSRange cut = [name rangeOfComposedCharacterSequencesForRange:NSMakeRange(0, kNameMax)];
            name = [[name substringWithRange:cut] stringByAppendingString:@"…"];
        }
        NSString *elapsed = formatMinutes((now * 1000 - [s[@"since"] doubleValue]) / 60000);
        CGFloat nameX = kPad + 16;
        CGFloat timeW = ceil([elapsed sizeWithAttributes:timeAttrs].width);
        CGFloat timeX = kWidth - kPad - timeW;
        CGFloat nameW = MIN(ceil([name sizeWithAttributes:nameAttrs].width), timeX - 6 - nameX);
        [self text:name at:NSMakeRect(nameX, y + 3, nameW, 16) color:primary right:NO mono:NO];
        [self text:elapsed at:NSMakeRect(timeX, y + 4, timeW, 16) color:secondary right:YES mono:YES];
        [self barFrom:nameX + nameW + 8 to:timeX - 8 y:y + 10 state:state now:now];
        y += kRow;
    }

    y += 4;
    [[NSColor colorWithWhite:1 alpha:0.82] setFill];
    NSRectFill(NSMakeRect(kPad, y, inner, 1));
    y += 5;

    if (self.usage == nil) {
        [self text:@"usage unavailable" at:NSMakeRect(kPad, y + 2, inner, 16) color:secondary right:NO mono:NO];
        return;
    }
    for (NSDictionary *u in self.usage) {
        double percent = [u[@"percent"] doubleValue];
        double resetsAt = [u[@"resetsAt"] doubleValue];
        NSColor *color = usageColor(percent);
        CGFloat barX = kPad + 24, barW = 110;

        [self text:u[@"label"] at:NSMakeRect(kPad, y + 3, 22, 16) color:secondary right:NO mono:YES];
        [[NSColor colorWithWhite:1 alpha:0.82] setFill];
        [[NSBezierPath bezierPathWithRoundedRect:NSMakeRect(barX, y + 8, barW, 5) xRadius:2.5 yRadius:2.5] fill];
        [color setFill];
        CGFloat filled = barW * MIN(MAX(percent, 0), 100) / 100;
        [[NSBezierPath bezierPathWithRoundedRect:NSMakeRect(barX, y + 8, filled, 5) xRadius:2.5 yRadius:2.5] fill];
        [self text:[NSString stringWithFormat:@"%.0f%%", percent] at:NSMakeRect(barX + barW + 4, y + 3, 36, 16) color:color right:YES mono:YES];
        if (resetsAt > 0) {
            NSString *left = formatMinutes((resetsAt - now * 1000) / 60000);
            [self text:[@"↻ " stringByAppendingString:left] at:NSMakeRect(kWidth - kPad - 52, y + 3, 52, 16) color:secondary right:YES mono:YES];
        }
        y += kUsageRow;
    }
}

@end

static NSPanel *panel;
static MonitorView *monitorView;
static NSStatusItem *statusItem;
static NSString *statusState; // what the menu bar dot currently shows

static void resizeToFit(void) {
    CGFloat height = [MonitorView heightForSessions:monitorView.sessions.count usage:monitorView.usage.count];
    NSRect frame = panel.frame;
    if (frame.size.height == height && frame.size.width == kWidth) return;
    // grow downwards: the top edge stays under the menu bar
    frame.origin.y += frame.size.height - height;
    frame.size = NSMakeSize(kWidth, height);
    [panel setFrame:frame display:YES];
    [panel invalidateShadow];
}

// placeUnderIcon puts the window below the menu bar icon, on the screen
// that has keyboard focus (each screen has its own menu bar).
static void placeUnderIcon(void) {
    NSScreen *target = NSScreen.mainScreen;
    NSWindow *iconWindow = statusItem.button.window;
    NSScreen *iconScreen = iconWindow.screen ?: target;
    CGFloat fromRight = NSMaxX(iconScreen.frame) - NSMidX(iconWindow.frame);
    NSRect visible = target.visibleFrame;
    CGFloat x = NSMaxX(target.frame) - fromRight - kWidth / 2;
    x = MIN(MAX(x, NSMinX(visible) + 8), NSMaxX(visible) - kWidth - 8);
    NSRect frame = panel.frame;
    frame.origin = NSMakePoint(x, NSMaxY(visible) - frame.size.height - 6);
    [panel setFrame:frame display:YES];
}

// makePanel builds a fresh window around monitorView. macOS can pin a window
// to a single desktop over time (e.g. after the displays sleep) and no flag
// reset undoes it, only a new window joins all desktops again.
static NSPanel *makePanel(void) {
    NSRect frame = NSMakeRect(0, 0, kWidth, [MonitorView heightForSessions:monitorView.sessions.count
                                                                     usage:monitorView.usage.count]);
    NSPanel *p = [[NSPanel alloc] initWithContentRect:frame
                                            styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
                                              backing:NSBackingStoreBuffered
                                                defer:NO];
    p.releasedWhenClosed = NO;
    p.floatingPanel = YES;
    p.level = NSStatusWindowLevel;
    p.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
                           NSWindowCollectionBehaviorStationary |
                           NSWindowCollectionBehaviorFullScreenAuxiliary;
    p.hidesOnDeactivate = NO;
    p.acceptsMouseMovedEvents = YES;
    p.opaque = NO;
    p.backgroundColor = NSColor.clearColor;
    p.hasShadow = YES;
    p.appearance = [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];

    NSVisualEffectView *background = [[NSVisualEffectView alloc] initWithFrame:frame];
    background.material = NSVisualEffectMaterialHUDWindow;
    background.blendingMode = NSVisualEffectBlendingModeBehindWindow;
    background.state = NSVisualEffectStateActive;
    background.wantsLayer = YES;
    background.layer.cornerRadius = 12;
    background.layer.masksToBounds = YES;
    p.contentView = background;

    [monitorView removeFromSuperview];
    monitorView.frame = background.bounds;
    [background addSubview:monitorView];
    return p;
}

static void showWindow(void) {
    NSPanel *old = panel;
    panel = makePanel();
    placeUnderIcon();
    [panel orderFrontRegardless];
    [old orderOut:nil];
}

static void toggleWindow(void) {
    if (panel.visible) {
        [panel orderOut:nil];
        return;
    }
    showWindow();
}

// aggregateState is the most urgent state among the sessions:
// blocked on you, then working, then done. nil when there are none.
static NSString *aggregateState(NSArray<NSDictionary *> *sessions) {
    NSString *result = nil;
    for (NSDictionary *s in sessions) {
        NSString *state = s[@"state"];
        if ([state isEqualToString:@"permission"] || [state isEqualToString:@"question"]) return state;
        if ([state isEqualToString:@"working"]) result = state;
        else if (result == nil) result = state;
    }
    return result;
}

static void updateStatusIcon(void) {
    NSString *state = aggregateState(monitorView.sessions);
    if (state == statusState || [state isEqualToString:statusState]) return;
    statusState = state;
    statusItem.button.image = [NSImage imageWithSize:NSMakeSize(18, 18)
                                             flipped:NO
                                      drawingHandler:^BOOL(NSRect rect) {
                                          NSRect dot = NSMakeRect(4, 4, 10, 10);
                                          if (state == nil) { // no sessions: hollow circle
                                              [[NSColor colorWithWhite:0.6 alpha:1] setStroke];
                                              NSBezierPath *ring = [NSBezierPath bezierPathWithOvalInRect:NSInsetRect(dot, 0.75, 0.75)];
                                              ring.lineWidth = 1.5;
                                              [ring stroke];
                                              return YES;
                                          }
                                          [stateColor(state) setFill];
                                          [[NSBezierPath bezierPathWithOvalInRect:dot] fill];
                                          return YES;
                                      }];
}

@interface Controller : NSObject
@end

@implementation Controller
- (void)statusClicked:(id)sender {
    if (NSApp.currentEvent.type != NSEventTypeRightMouseUp) {
        toggleWindow();
        return;
    }
    NSMenu *menu = [[NSMenu alloc] init];
    [menu addItemWithTitle:@"Quit cmon" action:@selector(terminate:) keyEquivalent:@""];
    NSStatusBarButton *button = statusItem.button;
    [menu popUpMenuPositioningItem:nil atLocation:NSMakePoint(0, NSHeight(button.bounds) + 4) inView:button];
}
@end

static Controller *controller;

static OSStatus hotKeyPressed(EventHandlerCallRef call, EventRef event, void *data) {
    toggleWindow();
    return noErr;
}

// registerHotKey toggles the window with F3 from any app. A Carbon hot key
// needs no Accessibility permission.
static void registerHotKey(void) {
    EventTypeSpec spec = {kEventClassKeyboard, kEventHotKeyPressed};
    InstallApplicationEventHandler(&hotKeyPressed, 1, &spec, NULL, NULL);
    EventHotKeyRef ref;
    EventHotKeyID hotKeyID = {'cmon', 1};
    RegisterEventHotKey(kVK_F3, 0, hotKeyID, GetApplicationEventTarget(), 0, &ref);
}

void cmon_run(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];

        monitorView = [[MonitorView alloc] initWithFrame:NSZeroRect];
        monitorView.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
        monitorView.hoveredRow = -1;
        panel = makePanel();

        // switching desktops with the window open: bring it along if it got pinned
        [NSWorkspace.sharedWorkspace.notificationCenter addObserverForName:NSWorkspaceActiveSpaceDidChangeNotification
                                                                    object:nil
                                                                     queue:NSOperationQueue.mainQueue
                                                                usingBlock:^(NSNotification *n) {
                                                                    if (panel.visible && !panel.onActiveSpace) showWindow();
                                                                }];

        controller = [[Controller alloc] init];
        statusItem = [NSStatusBar.systemStatusBar statusItemWithLength:NSSquareStatusItemLength];
        statusItem.button.target = controller;
        statusItem.button.action = @selector(statusClicked:);
        [statusItem.button sendActionOn:NSEventMaskLeftMouseUp | NSEventMaskRightMouseUp];
        statusState = @"";
        updateStatusIcon();
        registerHotKey();

        // the pulse of the working dot
        [NSTimer scheduledTimerWithTimeInterval:1.0 / 20
                                        repeats:YES
                                          block:^(NSTimer *t) {
                                              if (panel.visible && [monitorView hasWorking]) monitorView.needsDisplay = YES;
                                          }];
        [NSApp run];
    }
}

// cmon_update and cmon_play run on Go threads, which have no autorelease pool
// of their own: without one, every autoreleased object lives until the thread
// exits, and Go threads never do.
void cmon_update(const char *json) {
    @autoreleasepool {
        NSData *data = [NSData dataWithBytes:json length:strlen(json)];
        NSDictionary *snap = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
        if (![snap isKindOfClass:NSDictionary.class]) return;
        dispatch_async(dispatch_get_main_queue(), ^{
            id sessions = snap[@"sessions"], usage = snap[@"usage"];
            monitorView.sessions = [sessions isKindOfClass:NSArray.class] ? sessions : @[];
            monitorView.usage = [usage isKindOfClass:NSArray.class] ? usage : nil;
            resizeToFit();
            updateStatusIcon();
            monitorView.needsDisplay = YES;
        });
    }
}

void cmon_play(const char *name) {
    @autoreleasepool {
        NSString *sound = [NSString stringWithUTF8String:name];
        dispatch_async(dispatch_get_main_queue(), ^{
            NSSound *s = [NSSound soundNamed:sound];
            [s stop];
            [s play];
        });
    }
}
