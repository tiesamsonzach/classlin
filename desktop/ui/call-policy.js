// Decides what the classroom does with an incoming call.
// openCamera is true only when this side is idle and the server has verified
// that the caller is a logged-in admin allowed to call this room.
function decideIncomingCall(input) {
  var inCall = !!(input && input.inCall);
  var verified = !!(input && input.verified);
  if (inCall) {
    return { action: "busy", openCamera: false };
  }
  if (!verified) {
    return { action: "reject", openCamera: false };
  }
  return { action: "answer", openCamera: true };
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = { decideIncomingCall: decideIncomingCall };
}
