import { useAuthStore } from '@/store/auth';
import axios from 'axios';
import React from 'react';
import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { io, Socket } from 'socket.io-client';

export const WebSocketStatus = {
	CONNECTING: 'connecting',
	CONNECTED: 'connected',
	DISCONNECTED: 'disconnected',
} as const;

export type WebSocketStatus = (typeof WebSocketStatus)[keyof typeof WebSocketStatus];

interface WebSocketContextType {
	socket: Socket | null;
	status: WebSocketStatus;
}

const WebSocketContext = createContext<WebSocketContextType>({
	socket: null,
	status: WebSocketStatus.CONNECTING,
});

const apiUrl = import.meta.env.VITE_API_URL;


const noop = () => {};

export const WebSocketProvider = ({
	children,
}: {
	children: React.ReactNode;
}) => {
	const [socket, setSocket] = useState<Socket | null>(null);
	const [status, setStatus] = useState<WebSocketStatus>(
		WebSocketStatus.CONNECTING,
	);

  const token = useAuthStore((state) => state.accessToken);
  // Track if refresh has been attempted
  const refreshAttemptedRef = React.useRef(false);

	useEffect(() => {

		if (!token) {
			setStatus(WebSocketStatus.DISCONNECTED);
      refreshAttemptedRef.current = false;
			return noop;
		}

		const newSocket = io(apiUrl, {
			transports: ['websocket'],
			query: { token },
		});

		newSocket.on('connect', () => {
			setStatus(WebSocketStatus.CONNECTED);
			console.log('WebSocket: Connected');
      refreshAttemptedRef.current = false; // Reset on successful connect
		});

		newSocket.on('disconnect', (...args) => {
			setStatus(WebSocketStatus.DISCONNECTED);
			console.log(`WebSocket: Disconnected ${JSON.stringify(args)}}`);
		});

		newSocket.on('connect_error', async (args) => {
      // Only try refresh once per effect/token
      if (refreshAttemptedRef.current) {
        setStatus(WebSocketStatus.DISCONNECTED);
        return;
      }

			try {
				if (args?.message?.startsWith('Unauthorized')) {
					// refresh token flow
          refreshAttemptedRef.current = true;
          const refreshToken = useAuthStore.getState().refreshToken;

					if (!refreshToken) {
						throw new Error('No refresh token');
					}

          const { data } = await axios.post(
            `${import.meta.env.VITE_API_URL}/api/v1/auth/refresh`,
            { refreshToken },
            {
              headers: {
                "Content-Type": "application/json",
              },
            }
          );
					const newToken = data?.data.accessToken;
					const newRefreshToken = data?.data.refreshToken;
					if (!newToken) {
						throw new Error('No new token');
					}

					useAuthStore.getState().setTokens(newToken, newRefreshToken);

					return;
				}
			} catch (e) {
				console.error('Error refreshing token from WS', e);
        useAuthStore.getState().clearTokens();
			}

			console.error('WebSocket: Connection error:', args);
			setStatus(WebSocketStatus.DISCONNECTED);
		});

		setSocket(newSocket);

		return () => {
			newSocket.disconnect();
			setStatus(WebSocketStatus.DISCONNECTED);
      refreshAttemptedRef.current = false;
		};
	}, [token]); // Reconnect if the token changes

	const value = useMemo(() => {
		return { socket, status: status as WebSocketStatus };
	}, [socket, status]);

	return (
		<WebSocketContext.Provider value={value}>
			{children}
		</WebSocketContext.Provider>
	);
};

export const useWebSocket = () => {
  const ctx = useContext(WebSocketContext);
  if (!ctx) throw new Error("useWebSocket must be used within a WebSocketProvider");
  return ctx;
}
