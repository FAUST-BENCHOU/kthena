/*
Copyright The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package sessionsticky

// SessionStickyE2EModelServerReplicas scales the mock model server so no-session-key / disabled-sticky tests can see multiple backends.
// SessionStickyE2ERouterConfigurationYAML is the Helm post-render replacement for kthena-router-config (no prefix-cache; Score random-only).
const (
	SessionStickyE2EModelServerReplicas     int32 = 5
	SessionStickyE2ERouterConfigurationYAML       = `scheduler:
  pluginConfig:
  - name: least-request
    args:
      maxWaitingRequests: 10
  - name: least-latency
    args:
      TTFTTPOTWeightFactor: 0.5
  - name: kvcache-aware
    args:
      blockSizeToHash: 128
      maxBlocksToMatch: 128
  plugins:
    Filter:
      enabled:
        - least-request
      disabled:
        - lora-affinity
    Score:
      enabled:
        - name: random
          weight: 1
`
)
