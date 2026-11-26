//go:build darwin && cgo && (dev || production)

#import <Cocoa/Cocoa.h>
#include <stdint.h>

extern void ADBQDialogResponse(uintptr_t id, int response);

typedef NS_ENUM(int, ADBQDialogResponseCode) {
    ADBQDialogDismissed,
    ADBQDialogAccepted,
    ADBQDialogBusy,
};

@interface ADBQDialogPanel : NSPanel <NSWindowDelegate>
@property (nonatomic) uintptr_t requestID;
- (void)dismiss:(id)sender;
- (void)accept:(id)sender;
@end

static ADBQDialogPanel *activeDialog;

@implementation ADBQDialogPanel

- (BOOL)canBecomeMainWindow { return NO; }
- (BOOL)canBecomeKeyWindow { return YES; }

- (void)finish:(ADBQDialogResponseCode)response {
    if (activeDialog != self) return;
    activeDialog = nil;
    [self orderOut:nil];
    ADBQDialogResponse(self.requestID, response);
    [self autorelease];
}

- (void)dismiss:(id)sender { [self finish:ADBQDialogDismissed]; }
- (void)accept:(id)sender { [self finish:ADBQDialogAccepted]; }
- (void)cancelOperation:(id)sender { [self dismiss:sender]; }

- (BOOL)windowShouldClose:(NSWindow *)window {
    [self dismiss:window];
    return NO;
}

@end

static NSScreen *ADBQDialogScreen(void) {
    NSPoint mouse = [NSEvent mouseLocation];
    for (NSScreen *screen in NSScreen.screens) {
        if (NSPointInRect(mouse, screen.frame)) return screen;
    }
    return NSScreen.mainScreen;
}

static void ADBQPresentDialog(uintptr_t requestID, NSString *title, NSString *message,
                              NSString *dismiss, NSString *action, BOOL destructive) {
    if (activeDialog) {
        ADBQDialogResponse(requestID, ADBQDialogBusy);
        return;
    }

    NSScreen *screen = ADBQDialogScreen();
    NSRect available = screen.visibleFrame;
    CGFloat width = MIN(480, NSWidth(available) - 32);
    const CGFloat padding = 20;
    CGFloat bodyWidth = width - 2 * padding;

    ADBQDialogPanel *panel = [[ADBQDialogPanel alloc]
        initWithContentRect:NSMakeRect(0, 0, width, 200)
                  styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskNonactivatingPanel
                    backing:NSBackingStoreBuffered defer:NO screen:screen];
    activeDialog = panel;
    panel.requestID = requestID;
    panel.delegate = panel;
    panel.releasedWhenClosed = NO;
    panel.canHide = NO;
    panel.hidesOnDeactivate = NO;
    panel.level = NSFloatingWindowLevel;
    panel.collectionBehavior = NSWindowCollectionBehaviorMoveToActiveSpace | NSWindowCollectionBehaviorFullScreenAuxiliary;
    panel.title = title;
    panel.titleVisibility = NSWindowTitleHidden;

    NSButton *dismissButton = [NSButton buttonWithTitle:dismiss target:panel action:@selector(dismiss:)];
    dismissButton.keyEquivalent = @"\r";
    panel.defaultButtonCell = dismissButton.cell;

    NSMutableArray *buttons = [NSMutableArray array];
    if (action.length > 0) {
        NSButton *actionButton = [NSButton buttonWithTitle:action target:panel action:@selector(accept:)];
        if (@available(macOS 11.0, *)) actionButton.hasDestructiveAction = destructive;
        [buttons addObject:actionButton];
    }
    [buttons addObject:dismissButton];
    NSStackView *buttonRow = [NSStackView stackViewWithViews:buttons];
    buttonRow.orientation = NSUserInterfaceLayoutOrientationHorizontal;
    NSSize buttonSize = buttonRow.fittingSize;

    NSFont *bodyFont = [NSFont systemFontOfSize:NSFont.systemFontSize];
    NSMutableAttributedString *text = [[[NSMutableAttributedString alloc]
        initWithString:title attributes:@{NSFontAttributeName: [NSFont boldSystemFontOfSize:15],
                                         NSForegroundColorAttributeName: NSColor.labelColor}] autorelease];
    NSAttributedString *body = [[[NSAttributedString alloc]
        initWithString:[@"\n\n" stringByAppendingString:message]
            attributes:@{NSFontAttributeName: bodyFont, NSForegroundColorAttributeName: NSColor.labelColor}] autorelease];
    [text appendAttributedString:body];

    CGFloat bodyY = padding + buttonSize.height + 16;
    CGFloat maxBodyHeight = MIN(340, NSHeight(available) - bodyY - padding - 56);
    CGFloat measuredHeight = ceil([text boundingRectWithSize:NSMakeSize(bodyWidth, CGFLOAT_MAX)
        options:NSStringDrawingUsesLineFragmentOrigin | NSStringDrawingUsesFontLeading].size.height);
    BOOL scrollable = measuredHeight > maxBodyHeight;
    CGFloat bodyHeight = MIN(measuredHeight, maxBodyHeight);

    NSScrollView *scroll = [[[NSScrollView alloc] initWithFrame:NSMakeRect(padding, bodyY, bodyWidth, bodyHeight)] autorelease];
    scroll.drawsBackground = NO;
    scroll.hasVerticalScroller = scrollable;
    scroll.autohidesScrollers = YES;

    NSTextView *textView = [[[NSTextView alloc] initWithFrame:NSMakeRect(0, 0, scroll.contentSize.width, bodyHeight)] autorelease];
    textView.editable = NO;
    textView.drawsBackground = NO;
    textView.verticallyResizable = YES;
    textView.autoresizingMask = NSViewWidthSizable;
    textView.textContainerInset = NSZeroSize;
    textView.textContainer.lineFragmentPadding = 0;
    textView.textContainer.widthTracksTextView = YES;
    [textView.textStorage setAttributedString:text];
    scroll.documentView = textView;

    [panel setContentSize:NSMakeSize(width, bodyY + bodyHeight + padding)];
    [panel.contentView addSubview:scroll];
    buttonRow.frame = NSMakeRect(width - padding - buttonSize.width, padding, buttonSize.width, buttonSize.height);
    [panel.contentView addSubview:buttonRow];
    [panel setInitialFirstResponder:dismissButton];

    NSRect frame = panel.frame;
    [panel setFrameOrigin:NSMakePoint(floor(NSMidX(available) - NSWidth(frame) / 2),
                                      floor(NSMidY(available) - NSHeight(frame) / 2))];
    // The panel can take keyboard input without activating adbq or unhiding its windows.
    [panel orderFrontRegardless];
    [panel makeKeyWindow];
    [panel makeFirstResponder:dismissButton];
}

void ADBQShowDialog(uintptr_t id, const char *title, const char *message,
                    const char *dismiss, const char *action, int destructive) {
    @autoreleasepool {
        NSString *dialogTitle = [NSString stringWithUTF8String:title] ?: @"";
        NSString *dialogMessage = [NSString stringWithUTF8String:message] ?: @"";
        NSString *dismissTitle = [NSString stringWithUTF8String:dismiss] ?: @"";
        NSString *actionTitle = [NSString stringWithUTF8String:action] ?: @"";
        dispatch_async(dispatch_get_main_queue(), ^{
            ADBQPresentDialog(id, dialogTitle, dialogMessage, dismissTitle, actionTitle, destructive != 0);
        });
    }
}

void ADBQCancelDialog(uintptr_t id) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (activeDialog && activeDialog.requestID == id) [activeDialog dismiss:nil];
    });
}
