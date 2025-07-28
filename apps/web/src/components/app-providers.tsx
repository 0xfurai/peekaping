import { type ReactNode } from "react";
import { ThemeProvider } from "@/components/theme-provider";
import { TimezoneProvider } from "@/context/timezone-context";
import { WebSocketProvider } from "@/context/websocket-context";
import { VersionMismatchAlert } from "@/components/VersionMismatchAlert";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";

interface AppProvidersProps {
  children: ReactNode;
}

export const AppProviders = ({ children }: AppProvidersProps) => {
  const isDev = import.meta.env.MODE === "development";

  return (
    <ThemeProvider defaultTheme="dark" storageKey="peekaping-ui-theme">
      <TimezoneProvider>
        <VersionMismatchAlert />
        <WebSocketProvider>
          {children}
          {isDev && <ReactQueryDevtools initialIsOpen={false} />}
        </WebSocketProvider>
      </TimezoneProvider>
    </ThemeProvider>
  );
}; 