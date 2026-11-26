// After the user's workspaces changed on the server (they left one), refreshes /me and only then
// continues, since the next page reads /me: going on with a stale list could send the user to a
// workspace they are no longer in. When the refresh fails it does not continue; `onFailed` gets a
// `retry` that runs the refresh and the continuation again.
export const refreshMeThen = async ({
  refresh,
  proceed,
  onFailed,
}: {
  refresh: () => Promise<unknown>;
  proceed: () => void;
  onFailed: (retry: () => void) => void;
}): Promise<void> => {
  try {
    await refresh();
  } catch (error) {
    console.error("Failed to refresh the workspaces", error);
    onFailed(() => void refreshMeThen({ refresh, proceed, onFailed }));
    return;
  }
  proceed();
};
