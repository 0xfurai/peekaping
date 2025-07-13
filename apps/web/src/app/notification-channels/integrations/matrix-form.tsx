import { Input } from "@/components/ui/input";
import {
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
  FormDescription,
} from "@/components/ui/form";
import { z } from "zod";
import { Textarea } from "@/components/ui/textarea";
import { useFormContext } from "react-hook-form";

export const schema = z.object({
  type: z.literal("matrix"),
  homeserver_url: z.string().url({ message: "Valid homeserver URL is required" }),
  internal_room_id: z.string().min(1, { message: "Internal Room ID is required" }),
  access_token: z.string().min(1, { message: "Access Token is required" }),
  custom_message: z.string().optional(),
});

export const defaultValues = {
  type: "matrix" as const,
  homeserver_url: "",
  internal_room_id: "",
  access_token: "",
  custom_message: "{{ msg }}",
};

export const displayName = "Matrix";

export default function MatrixForm() {
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="homeserver_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              Homeserver URL <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="https://matrix.org"
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> Required
              <br />
              The URL of your Matrix homeserver (e.g., https://matrix.org)
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="internal_room_id"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              Internal Room ID <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="!roomid:matrix.org"
                type="text"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> Required
              <br />
              The internal room ID where notifications will be sent. This should start with "!" and look like: !roomid:matrix.org
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="access_token"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              Access Token <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="Your Matrix access token"
                type="password"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> Required
              <br />
              Your Matrix access token for authentication
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="custom_message"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Custom Message</FormLabel>
            <FormControl>
              <Textarea
                placeholder="Alert: {{ monitor.name }} is {{ status }}"
                className="min-h-[100px]"
                {...field}
              />
            </FormControl>
            <FormDescription>
              Custom message template. Available variables: {"{{ msg }}"}, {"{{ monitor.name }}"}, {"{{ status }}"}, {"{{ heartbeat.* }}"}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <div className="space-y-4 p-4 bg-blue-50 dark:bg-blue-900/20 rounded-lg border border-blue-200 dark:border-blue-800">
        <p className="text-sm text-blue-800 dark:text-blue-200">
          <strong>Setup Instructions:</strong>
        </p>
        <div className="space-y-2 text-sm text-blue-800 dark:text-blue-200">
          <p>
            1. Create a Matrix account for your bot (or use your existing account)
          </p>
          <p>
            2. Create or join the room where you want to receive notifications
          </p>
          <p>
            3. Get your access token by making a login request:
          </p>
          <div className="bg-blue-100 dark:bg-blue-800 p-2 rounded font-mono text-xs overflow-x-auto">
            <code>
              curl -XPOST -d '{`"type": "m.login.password", "identifier": {"user": "botusername", "type": "m.id.user"}, "password": "passwordforuser"`}' "https://home.server/_matrix/client/v3/login"
            </code>
          </div>
          <p>
            4. The response will contain an access_token field - use this as your Access Token
          </p>
          <p>
            5. Find your room's internal ID (starts with "!") in your Matrix client room settings
          </p>
        </div>
      </div>
    </>
  );
}
