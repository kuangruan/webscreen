function homeButton() {
    // KEYCODE_HOME = 3
    const KEYCODE_HOME = 3;
    
    // Send Down
    let p = createKeyPacket(TYPE_KEY_ACTION_DOWN, KEYCODE_HOME);
    sendWSMessage(p);

    // Send Up
    p = createKeyPacket(TYPE_KEY_ACTION_UP, KEYCODE_HOME);
    sendWSMessage(p);
}

function volumeUpButton() {
    const KEYCODE_VOLUME_UP = 24;
    let p = createKeyPacket(TYPE_KEY_ACTION_DOWN, KEYCODE_VOLUME_UP);
    sendWSMessage(p);
    p = createKeyPacket(TYPE_KEY_ACTION_UP, KEYCODE_VOLUME_UP);
    sendWSMessage(p);
}

function volumeDownButton() {
    const KEYCODE_VOLUME_DOWN = 25;
    let p = createKeyPacket(TYPE_KEY_ACTION_DOWN, KEYCODE_VOLUME_DOWN);
    sendWSMessage(p);
    p = createKeyPacket(TYPE_KEY_ACTION_UP, KEYCODE_VOLUME_DOWN);
    sendWSMessage(p);
}

function powerButton() {
    const KEYCODE_POWER = 26;
    let p = createKeyPacket(TYPE_KEY_ACTION_DOWN, KEYCODE_POWER);
    sendWSMessage(p);
    p = createKeyPacket(TYPE_KEY_ACTION_UP, KEYCODE_POWER);
    sendWSMessage(p);
}

function checkOrientation() {
    const video = document.getElementById('remoteVideo');
    if (!video.videoWidth || !video.videoHeight) return;

    const isPagePortrait = window.innerHeight > window.innerWidth;
    const isVideoLandscape = video.videoWidth > video.videoHeight;

    const isPageLandscape = window.innerWidth > window.innerHeight;
    const isVideoPortrait = video.videoHeight > video.videoWidth;

    if (isPagePortrait && isVideoLandscape) {
            // console.log("Auto-rotating: Page Portrait, Video Landscape");
            p = createRotatePacket();
            sendWSMessage(p);
        } else if (isPageLandscape && isVideoPortrait) {
            // console.log("Auto-rotating: Page Landscape, Video Portrait");
            p = createRotatePacket();
            sendWSMessage(p);
        }
}