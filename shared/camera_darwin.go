//go:build darwin && cgo

package shared

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AVFoundation -framework Foundation

#import <AVFoundation/AVFoundation.h>

// ensureCameraAccess returns 0 when access is authorized (possibly after
// prompting the user) and 1 when access is denied or restricted.
static int ensureCameraAccess(void) {
	AVAuthorizationStatus st = [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeVideo];
	if (st == AVAuthorizationStatusAuthorized) {
		return 0;
	}
	if (st == AVAuthorizationStatusDenied || st == AVAuthorizationStatusRestricted) {
		return 1;
	}
	dispatch_semaphore_t sem = dispatch_semaphore_create(0);
	__block BOOL allowed = NO;
	[AVCaptureDevice requestAccessForMediaType:AVMediaTypeVideo
	                         completionHandler:^(BOOL granted) {
		allowed = granted;
		dispatch_semaphore_signal(sem);
	}];
	dispatch_semaphore_wait(sem, DISPATCH_TIME_FOREVER);
	return allowed ? 0 : 1;
}
*/
import "C"

import "fmt"

// EnsureCameraPermission makes sure the control panel holds macOS camera
// authorization before a video child process is spawned. Child processes
// (video module, ffmpeg probes) are attributed to the control panel by TCC,
// so the prompt must be answered here or every camera probe times out.
// Blocks until the user answers the prompt; must not run on the Fyne main
// goroutine.
func EnsureCameraPermission() error {
	if C.ensureCameraAccess() == 0 {
		return nil
	}
	return fmt.Errorf("camera access is denied for the OWLCMS control panel.\n" +
		"Enable it in System Settings → Privacy & Security → Camera, then launch the video module again")
}
