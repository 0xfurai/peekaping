import type { MonitorMonitorResponseDto } from "@/api";

// Type for navigation state to ensure type safety when passing clone data
export interface MonitorNavigationState {
  cloneData?: MonitorMonitorResponseDto;
}
