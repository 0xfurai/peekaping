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
import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
  SelectValue,
} from "@/components/ui/select";
import { useFormContext } from "react-hook-form";
import * as React from "react";

export const schema = z.object({
  type: z.literal("ntfy"),
  server_url: z.string().url({ message: "Valid NTFY server URL is required" }),
  topic: z.string().min(1, { message: "Topic is required" }),
  authentication_type: z.enum(["none", "basic", "token"]),
  username: z.string().optional(),
  password: z.string().optional(),
  token: z.string().optional(),
  priority: z.coerce.number().min(1).max(5),
  tags: z.string().optional(),
  title: z.string().optional(),
  custom_message: z.string().optional(),
});

export type NtfyFormValues = z.infer<typeof schema>;

export const defaultValues: NtfyFormValues = {
  type: "ntfy",
  server_url: "https://ntfy.sh",
  topic: "peekaping",
  authentication_type: "none",
  username: "",
  password: "",
  token: "",
  priority: 3,
  tags: "peekaping,monitoring",
  title: "Peekaping Alert - {{ name }}",
  custom_message: "{{ msg }}",
};

export const displayName = "NTFY";

export default function NtfyForm() {
  const form = useFormContext();
  const authType = form.watch("authentication_type");

  // Handle conditional validation
  React.useEffect(() => {
    if (authType === "basic") {
      form.clearErrors(["username", "password"]);
      if (!form.getValues("username")) {
        form.setError("username", {
          message: "Username is required for basic authentication",
        });
      }
      if (!form.getValues("password")) {
        form.setError("password", {
          message: "Password is required for basic authentication",
        });
      }
    } else if (authType === "token") {
      form.clearErrors(["token"]);
      if (!form.getValues("token")) {
        form.setError("token", {
          message: "Token is required for token authentication",
        });
      }
    }
  }, [authType, form]);

  return (
    <>
      <FormField
        control={form.control}
        name="server_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>NTFY Server URL</FormLabel>
            <FormControl>
              <Input
                placeholder="https://ntfy.sh"
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              The URL of your NTFY server. Use https://ntfy.sh for the public
              instance.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="topic"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Topic</FormLabel>
            <FormControl>
              <Input placeholder="peekaping" required {...field} />
            </FormControl>
            <FormDescription>
              The topic name for your notifications. This will be the channel
              where notifications are sent.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="authentication_type"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Authentication Type</FormLabel>
            <Select
              onValueChange={(val) => {
                if (!val) {
                  return;
                }
                field.onChange(val);
              }}
              value={field.value}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder="Select authentication type" />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="none">No Authentication</SelectItem>
                <SelectItem value="basic">Basic Authentication</SelectItem>
                <SelectItem value="token">Token Authentication</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              {authType === "none" && (
                <>
                  No authentication required. Suitable for public NTFY servers.
                </>
              )}
              {authType === "basic" && (
                <>Use username and password for basic authentication.</>
              )}
              {authType === "token" && (
                <>Use an access token for authentication.</>
              )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      {authType === "basic" && (
        <>
          <FormField
            control={form.control}
            name="username"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Username</FormLabel>
                <FormControl>
                  <Input placeholder="your-username" required {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="password"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Password</FormLabel>
                <FormControl>
                  <Input
                    placeholder="your-password"
                    type="password"
                    required
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </>
      )}

      {authType === "token" && (
        <FormField
          control={form.control}
          name="token"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Access Token</FormLabel>
              <FormControl>
                <Input
                  placeholder="tk_AgQdq7mVBoFD37zQVN29RhuMzNIz2"
                  type="password"
                  required
                  {...field}
                />
              </FormControl>
              <FormDescription>
                Your NTFY access token for authentication.
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}

      <FormField
        control={form.control}
        name="priority"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Priority</FormLabel>
            <Select
              onValueChange={(val) => {
                if (!val) {
                  return;
                }
                field.onChange(parseInt(val));
              }}
              value={field.value?.toString()}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder="Select priority" />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="1">1 - Min (Lowest)</SelectItem>
                <SelectItem value="2">2 - Low</SelectItem>
                <SelectItem value="3">3 - Default</SelectItem>
                <SelectItem value="4">4 - High</SelectItem>
                <SelectItem value="5">5 - Urgent (Highest)</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              The priority level of the notification. Higher priorities may
              trigger different behaviors on the client.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="tags"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Tags</FormLabel>
            <FormControl>
              <Input placeholder="peekaping,monitoring,alert" {...field} />
            </FormControl>
            <FormDescription>
              Comma-separated tags for categorizing notifications. Available
              variables: {"{{ name }}"}, {"{{ status }}"}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="title"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Custom Title</FormLabel>
            <FormControl>
              <Input placeholder="Peekaping Alert - {{ name }}" {...field} />
            </FormControl>
            <FormDescription>
              Custom title for the notification. Available variables:{" "}
              {"{{ name }}"}, {"{{ status }}"}, {"{{ msg }}"}
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
                placeholder="{{ msg }}"
                className="min-h-[100px]"
                {...field}
              />
            </FormControl>
            <FormDescription>
              Custom message content. Available variables: {"{{ msg }}"},{" "}
              {"{{ name }}"}, {"{{ status }}"}, {"{{ monitor.* }}"}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
