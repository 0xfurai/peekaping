import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import dayjs from "dayjs";
import { toast } from "sonner";
import { isAxiosError } from "axios";
import type { QueryClient } from "@tanstack/react-query";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export const isJson = (val: string) => {
  if (!val) return true;
  try {
    JSON.parse(val);
    return true;
  } catch {
    return false;
  }
};

export const isValidXml = (val: string) => {
  if (!val.trim()) return true; // Allow empty body
  try {
    const parser = new DOMParser();
    const doc = parser.parseFromString(val, "text/xml");
    const parserError = doc.querySelector("parsererror");
    return !parserError;
  } catch {
    return false;
  }
};

export const isValidForm = (val: string): boolean => {
  const trimmed = val.trim();
  if (!trimmed) return true; // Allow empty body

  // Accept simple URL-encoded format like "key=value"
  const isURLEncoded = /^[^=&]+=[^=&]*(&[^=&]+=[^=&]*)*$/.test(trimmed);

  if (isURLEncoded) return true;

  // return isJson(trimmed);
  return false;
};

export const convertToDateTimeLocal = (dateString?: string): string => {
  if (!dateString) return "";

  try {
    const date = dayjs(dateString);
    if (!date.isValid()) return "";

    // Format as YYYY-MM-DDTHH:mm for datetime-local input
    return date.format("YYYY-MM-DDTHH:mm");
  } catch (error) {
    console.error("Error converting date to datetime-local format:", error);
    return "";
  }
};

export const convertToUtc = (dateString?: string): string => {
  if (!dateString) return "";
  return dateString + ":00Z";
};

export const last = <T>(arr: T[]): T | undefined => {
  return arr[arr.length - 1];
};

export const commonMutationErrorHandler =
  (fallbackMessage: string) => (error: unknown) => {
    if (isAxiosError(error)) {
      toast.error(error.response?.data.message || error.message || fallbackMessage);
    } else {
      toast.error(fallbackMessage);
    }
    console.error(error);
  };


export const invalidateByPartialQueryKey = (queryClient: QueryClient, part: Record<string, string>) => {
  queryClient.invalidateQueries({
    predicate: (query) => {
      if (!Array.isArray(query.queryKey) || !query.queryKey[0]) {
        return false;
      }

      const queryKeyObj = query.queryKey[0] as Record<string, unknown>;

      return Object.entries(part).every(([key, value]) =>
        queryKeyObj[key] === value
      );
    },
  });
};
