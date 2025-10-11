import dayjs from 'dayjs';
import utc from 'dayjs/plugin/utc';
import timezone from 'dayjs/plugin/timezone';

// Initialize dayjs plugins
dayjs.extend(utc);
dayjs.extend(timezone);

/**
 * Convert datetime-local input to UTC using specified timezone
 * @param dateString - datetime-local string in format "YYYY-MM-DDTHH:MM"
 * @param timezone - timezone string (e.g., "America/New_York")
 * @returns UTC ISO string
 */
export const convertDateTimeLocalToUTC = (dateString: string, timezone: string): string => {
  if (!dateString) return "";
  
  try {
    // Parse the datetime-local string in the specified timezone and convert to UTC
    return dayjs.tz(dateString, timezone).utc().format("YYYY-MM-DDTHH:mm:ss[Z]");
  } catch (error) {
    console.error("Error converting datetime-local to UTC:", error);
    return dateString + ":00Z"; // Fallback to simple append
  }
};

/**
 * Convert UTC date to datetime-local format for input fields
 * @param utcDateString - UTC ISO string
 * @param timezone - target timezone
 * @returns datetime-local string in format "YYYY-MM-DDTHH:MM"
 */
export const convertUTCToDateTimeLocal = (utcDateString: string, timezone: string): string => {
  if (!utcDateString) return "";
  
  try {
    return dayjs.utc(utcDateString).tz(timezone).format("YYYY-MM-DDTHH:mm");
  } catch (error) {
    console.error("Error converting UTC to datetime-local:", error);
    return "";
  }
};
