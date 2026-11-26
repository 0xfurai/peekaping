// A workspace an owner scheduled for deletion (docs/adr/0005, GLOSSARY "Scheduled deletion"): /me
// carries the date on every member's workspace, and only an owner can cancel.

export type DeletionNotice = { scheduledAt: string; canCancel: boolean };

/**
 * What the banner and the settings card tell about a workspace's scheduled deletion: the date, and
 * whether this user may cancel it. Null while no deletion is scheduled.
 */
export const deletionNotice = (
  workspace: { role?: string; deletionScheduledAt?: string | null } | undefined
): DeletionNotice | null => {
  if (!workspace?.deletionScheduledAt) return null;
  return { scheduledAt: workspace.deletionScheduledAt, canCancel: workspace.role === "owner" };
};

/** The date and time of the deletion in the user's language and time zone (the browser's by default). */
export const formatDeletionDate = (scheduledAt: string, locale: string, timeZone?: string): string => {
  const options: Intl.DateTimeFormatOptions = { dateStyle: "long", timeStyle: "short", timeZone };
  try {
    return new Intl.DateTimeFormat(locale, options).format(new Date(scheduledAt));
  } catch {
    // An unknown language tag throws a RangeError
    return new Intl.DateTimeFormat("en-US", options).format(new Date(scheduledAt));
  }
};
