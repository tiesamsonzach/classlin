import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createContext, runInContext } from "node:vm";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const desktop = readFileSync(join(root, "desktop/ui/call-policy.js"), "utf8");
const web = readFileSync(join(root, "server/internal/web/static/call-policy.js"), "utf8");
assert.equal(desktop, web);

const context = { module: { exports: {} }, exports: {} };
createContext(context);
runInContext(desktop, context);
const decide = context.decideIncomingCall;

function same(actual, expected) {
  assert.equal(JSON.stringify(actual), JSON.stringify(expected));
}
same(decide({ inCall: true, verified: true }), { action: "busy", openCamera: false });
same(decide({ inCall: false, verified: false }), { action: "reject", openCamera: false });
same(decide(null), { action: "reject", openCamera: false });
same(decide({ inCall: false, verified: true }), { action: "answer", openCamera: true });
