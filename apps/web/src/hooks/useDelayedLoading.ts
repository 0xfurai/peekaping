import { useState, useEffect } from 'react';

export const useDelayedLoading = (isLoading: boolean, delay: number = 200): boolean => {
  const [delayedLoading, setDelayedLoading] = useState(false);

  useEffect(() => {
    let timeoutId: NodeJS.Timeout;

    if (isLoading) {
      // Start the delay timer
      timeoutId = setTimeout(() => {
        setDelayedLoading(true);
      }, delay);
    } else {
      // Reset immediately when loading stops
      setDelayedLoading(false);
    }

    return () => {
      if (timeoutId) {
        clearTimeout(timeoutId);
      }
    };
  }, [isLoading, delay]);

  return delayedLoading;
};
