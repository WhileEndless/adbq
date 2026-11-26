//go:build darwin && cgo && (dev || production)

#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>

extern char *ADBQMenuSnapshot(void);
extern void ADBQMenuClick(long);

@interface ADBQStatusMenu : NSObject <NSMenuDelegate>
@property (retain) NSStatusItem *statusItem;
- (void)addRows:(NSArray *)rows toMenu:(NSMenu *)menu;
- (void)runItem:(NSMenuItem *)item;
@end

@implementation ADBQStatusMenu

- (void)menuNeedsUpdate:(NSMenu *)menu {
    char *snapshot = ADBQMenuSnapshot();
    if (snapshot == NULL) return;
    NSData *data = [NSData dataWithBytes:snapshot length:strlen(snapshot)];
    free(snapshot);
    NSArray *rows = [NSJSONSerialization JSONObjectWithData:data options:0 error:NULL];
    [menu removeAllItems];
    if (rows != nil) [self addRows:rows toMenu:menu];
}

- (void)addRows:(NSArray *)rows toMenu:(NSMenu *)menu {
    menu.autoenablesItems = NO;
    for (NSDictionary *row in rows) {
        if ([row[@"separator"] boolValue]) {
            [menu addItem:[NSMenuItem separatorItem]];
            continue;
        }
        NSNumber *identifier = row[@"id"];
        NSMenuItem *item = [[[NSMenuItem alloc] initWithTitle:row[@"title"]
                                                    action:identifier ? @selector(runItem:) : NULL
                                             keyEquivalent:@""] autorelease];
        item.enabled = ![row[@"disabled"] boolValue];
        item.state = [row[@"checked"] boolValue] ? NSControlStateValueOn : NSControlStateValueOff;
        if (identifier) {
            item.target = self;
            item.tag = identifier.integerValue;
        }
        NSString *symbol = row[@"icon"];
        if (@available(macOS 11.0, *)) {
            if (symbol) {
                NSImage *icon = [NSImage imageWithSystemSymbolName:symbol accessibilityDescription:nil];
                item.image = [icon imageWithSymbolConfiguration:
                    [NSImageSymbolConfiguration configurationWithPointSize:13 weight:NSFontWeightRegular]];
            }
        }
        NSArray *children = row[@"children"];
        if (children.count > 0) {
            NSMenu *submenu = [[[NSMenu alloc] initWithTitle:item.title] autorelease];
            [self addRows:children toMenu:submenu];
            item.submenu = submenu;
        }
        [menu addItem:item];
    }
}

- (void)runItem:(NSMenuItem *)item {
    ADBQMenuClick(item.tag);
}

- (void)dealloc {
    [_statusItem release];
    [super dealloc];
}

@end

// Reduce adbq's phone and terminal mark to a monochrome template so AppKit
// can tint it for menu bar appearance, accent, and selection.
static NSImage *ADBQStatusIcon(void) {
    NSImage *icon = [NSImage imageWithSize:NSMakeSize(18, 18) flipped:NO drawingHandler:^BOOL(NSRect rect) {
        [[NSColor blackColor] setStroke];
        NSBezierPath *phone = [NSBezierPath bezierPathWithRoundedRect:NSMakeRect(3.5, 1.5, 11, 15) xRadius:3 yRadius:3];
        phone.lineWidth = 1.35;
        [phone stroke];

        NSBezierPath *prompt = [NSBezierPath bezierPath];
        prompt.lineWidth = 1.35;
        prompt.lineCapStyle = NSLineCapStyleRound;
        prompt.lineJoinStyle = NSLineJoinStyleRound;
        [prompt moveToPoint:NSMakePoint(6.0, 11.2)];
        [prompt lineToPoint:NSMakePoint(8.6, 9.1)];
        [prompt lineToPoint:NSMakePoint(6.0, 7.0)];
        [prompt moveToPoint:NSMakePoint(9.8, 7.0)];
        [prompt lineToPoint:NSMakePoint(11.9, 7.0)];
        [prompt stroke];
        return YES;
    }];
    icon.template = YES;
    return icon;
}

static ADBQStatusMenu *controller;

void ADBQMountMenu(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (controller) return;
        // Wails launches as a regular app. AppKit can suppress a regular
        // app's status menu in another app's full-screen Space.
        if (![NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory]) {
            NSLog(@"adbq: could not switch to menu bar activation policy");
        }
        controller = [[ADBQStatusMenu alloc] init];
        controller.statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength];
        NSStatusBarButton *button = controller.statusItem.button;
        button.image = ADBQStatusIcon();
        button.toolTip = @"adbq quick actions";
        [button setAccessibilityLabel:@"adbq quick actions"];
        NSMenu *menu = [[[NSMenu alloc] initWithTitle:@"adbq"] autorelease];
        menu.delegate = controller;
        controller.statusItem.menu = menu;
        [controller menuNeedsUpdate:menu];
    });
}

void ADBQUnmountMenu(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!controller) return;
        controller.statusItem.menu.delegate = nil;
        [[NSStatusBar systemStatusBar] removeStatusItem:controller.statusItem];
        [controller release];
        controller = nil;
    });
}
