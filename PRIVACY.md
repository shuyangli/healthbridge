# HealthBridge Privacy Policy

_Last updated: 2026-04-07_

This document describes what data HealthBridge collects, where it goes,
and what control you have over it. The same text is shipped inside the
iOS app as the in-app disclosure required by App Store guideline 5.1.2(i).

## What HealthBridge does

HealthBridge is a tool that lets a local AI agent running on your Mac
read and write Apple Health data on your iPhone. There are three pieces:

1. **`healthbridge` (the CLI)** runs on your Mac alongside the agent.
2. **HealthBridge (the iOS app)** owns access to HealthKit on your iPhone.
3. **`healthbridge-relay`** is a tiny Cloudflare Worker that brokers
   encrypted messages between the two.

## What data is processed

- **HealthKit data you grant access to.** This includes whichever sample
  types you authorise during pairing — for example step count, workouts,
  nutrition, heart rate. The iOS app only requests scopes for the types
  you have granted.
- **HealthKit profile characteristics.** If you grant access, the agent
  can also read your date of birth, biological sex, blood type,
  Fitzpatrick skin type, wheelchair-use preference, and activity move
  mode via the `healthbridge profile` subcommand. These are read-only
  fields you set in the Health app and never expire; the agent uses
  them to ground fitness-coaching answers (e.g. age, sex-adjusted
  basal energy estimates). You can decline any of these in the
  HealthKit auth sheet at pairing time.
- **Pairing material.** A random 26-character pair ID, two X25519
  public keys, and a 32-byte Bearer token issued by the relay.
- **Audit log.** The iOS app keeps a local list of every job the CLI
  has executed against your Health data, including the sample type and
  outcome. The audit log lives in the app's document directory and is
  not transmitted anywhere.

## How the relay sees data

**HealthBridge shares your Health data with the AI agent on your paired
Mac, and only with that agent.** The data passes through the relay on
its way between your iPhone and your Mac, but the relay only sees
encrypted bytes — it cannot read what is inside any job or result.

Specifically:

- Every job and result is encrypted with **ChaCha20-Poly1305** using a
  32-byte session key derived from an X25519 key exchange that happens
  at pairing time. The session key never leaves your iPhone or your Mac.
- The relay receives only ciphertext blobs and minimal routing metadata
  (the pair ID, blob sizes, and timestamps). It cannot decrypt the
  payloads.
- A short 6-digit code (the SAS) is shown on both your iPhone and your
  Mac during pairing. You confirm they match before pairing completes;
  this defeats any tampering at the relay.

The relay holds job blobs for at most 7 days and result blobs for at
most 24 hours. After that they are deleted, regardless of whether the
iPhone has come online to drain them.

## What is NOT collected

- **No advertising or marketing data.** HealthBridge has no ads, no
  trackers, and no analytics.
- **No iCloud sync of HealthKit data.** Apple's HealthKit policy
  prohibits this and HealthBridge respects it.
- **No third-party services other than the relay.** No Google Analytics,
  no Crashlytics, no telemetry.
- **No data shared with third-party AI other than the agent on your
  paired Mac**, which you have explicitly authorised at pairing time.

## Your control

- **At pairing time** you choose which HealthKit sample types to grant
  to your Mac. You can revoke any subset later from the Activity log
  screen.
- **Revoking the pair** drops the relay mailbox and the on-device
  consent record; future requests from that Mac will be rejected. You
  can re-pair if you change your mind.
- **The audit log** is fully readable in the app, and a one-tap export
  produces a JSON file you can review or back up.
- **Deleting the app** removes every byte of HealthBridge data from
  your iPhone, including the pair record, the audit log, and the
  session key.

## Compliance

HealthBridge is designed to comply with Apple App Store guideline
5.1.2(i) (third-party AI disclosure), the long-standing HealthKit data
sharing rules (no advertising use, no iCloud, no false data writes),
and the European GDPR's data minimisation principle. If you believe
HealthBridge is mishandling your data, please open an issue at
https://github.com/shuyangli/healthbridge or email the address shown in
the App Store listing.

## Changes to this policy

If we need to change what HealthBridge does with your data, we will
update this file in the source repository, change the in-app disclosure
text, and bump the app version. The "Last updated" date at the top is
authoritative.
