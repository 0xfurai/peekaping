export function formatDateToTimezone(
  date: string | Date,
  timezone: string,
  options?: Intl.DateTimeFormatOptions
): string {
  const d = typeof date === 'string' ? new Date(date) : date;
  const fmt = new Intl.DateTimeFormat('en-US', {
    // year: 'numeric',
    // month: '2-digit',
    // day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    // second: '2-digit',
    timeZone: timezone,
    hour12: false,
    ...options,
  });
  return fmt.format(d);
}
