// The request that marks a probe as a default probe, or takes the mark away (PUT /probes/{id}).
// A field left out of the body stays as it is, so it carries is_default alone.
export const setProbeDefaultRequest = (probeId: string, isDefault: boolean) => ({
  path: { id: probeId },
  body: { is_default: isDefault },
});
