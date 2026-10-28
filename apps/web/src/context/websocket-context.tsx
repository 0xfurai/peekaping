import { getConfig } from '@/lib/config';
import { useAuthStore } from '@/store/auth';
import axios from 'axios';
import React from 'react';
import { createContext, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { io, Socket } from 'socket.io-client';
import { useLocation } from 'react-router-dom';
import { heartbeatEvent, subscribeToRoom, workspaceRoom } from '@/lib/socket-rooms';
import { workspaceIdFromPathname } from '@/lib/workspace-path';

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

const apiUrl = getConfig().API_URL


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
			// The server reads the token from the auth payload, not the query string,
			// which proxies write to their access logs
			auth: { token },
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
            `${getConfig().API_URL}/api/v1/auth/refresh`,
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

/**
 * Subscribes to a socket room's event while the component is mounted. Joins the room when
 * the first subscriber arrives and again after every reconnect, and leaves it when the last
 * one goes, without leaving a room that a re-run of the caller's effect stays in. The handler
 * is read through a ref, so a new callback on each render does not resubscribe. A null room
 * subscribes to nothing.
 */
export const useSocketRoom = <A extends unknown[]>(
  room: string | null | undefined,
  event: string,
  handler: (...args: A) => void,
) => {
  const { socket } = useWebSocket();
  const handlerRef = useRef(handler);

  useEffect(() => {
    handlerRef.current = handler;
  });

  useEffect(() => {
    if (!socket || !room) return;
    return subscribeToRoom(socket, room, event, (...args: A) => handlerRef.current(...args));
  }, [socket, room, event]);
};

/**
 * Subscribes to the heartbeats of every monitor in the workspace the address names, through that
 * workspace's room. The room is joined on connect and again after a reconnect, and left when the
 * address moves to another workspace or the caller unmounts, so the socket is only ever in the room
 * of the workspace on screen.
 */
export const useWorkspaceHeartbeats = <A extends unknown[]>(handler: (...args: A) => void) => {
  const { pathname } = useLocation();
  const workspaceId = workspaceIdFromPathname(pathname);
  const room = workspaceId ? workspaceRoom(workspaceId) : null;
  useSocketRoom(room, room ? heartbeatEvent(room) : '', handler);
};
