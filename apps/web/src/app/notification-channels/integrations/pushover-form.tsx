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
import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
  SelectValue,
} from "@/components/ui/select";
import { useFormContext } from "react-hook-form";

export const schema = z.object({
  type: z.literal("pushover"),
  pushover_user_key: z.string().min(1, { message: "User key is required" }),
  pushover_app_token: z.string().min(1, { message: "Application token is required" }),
  pushover_device: z.string().optional(),
  pushover_title: z.string().optional(),
  pushover_priority: z.coerce.number().min(-2).max(2).optional(),
  pushover_sounds: z.string().optional(),
  pushover_sounds_up: z.string().optional(),
  pushover_ttl: z.coerce.number().min(0).optional(),
});

export type PushoverFormValues = z.infer<typeof schema>;

export const defaultValues: PushoverFormValues = {
  type: "pushover",
  pushover_user_key: "",
  pushover_app_token: "",
  pushover_device: "",
  pushover_title: "",
  pushover_priority: 0,
  pushover_sounds: "pushover",
  pushover_sounds_up: "pushover",
  pushover_ttl: 0,
};

export const displayName = "Pushover";

const soundOptions = [
  { value: "pushover", label: "Pushover (default)" },
  { value: "bike", label: "Bike" },
  { value: "bugle", label: "Bugle" },
  { value: "cashregister", label: "Cash Register" },
  { value: "classical", label: "Classical" },
  { value: "cosmic", label: "Cosmic" },
  { value: "falling", label: "Falling" },
  { value: "gamelan", label: "Gamelan" },
  { value: "incoming", label: "Incoming" },
  { value: "intermission", label: "Intermission" },
  { value: "magic", label: "Magic" },
  { value: "mechanical", label: "Mechanical" },
  { value: "pianobar", label: "Piano Bar" },
  { value: "siren", label: "Siren" },
  { value: "spacealarm", label: "Space Alarm" },
  { value: "tugboat", label: "Tugboat" },
  { value: "alien", label: "Alien" },
  { value: "climb", label: "Climb" },
  { value: "persistent", label: "Persistent" },
  { value: "echo", label: "Echo" },
  { value: "updown", label: "Up Down" },
  { value: "vibrate", label: "Vibrate" },
  { value: "none", label: "None" },
];

const priorityOptions = [
  { value: -2, label: "Lowest" },
  { value: -1, label: "Low" },
  { value: 0, label: "Normal" },
  { value: 1, label: "High" },
  { value: 2, label: "Emergency" },
];

export default function PushoverForm() {
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="pushover_user_key"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              User Key <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="Your Pushover user key"
                type="password"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> Required
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_app_token"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              Application Token <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="Your Pushover application token"
                type="password"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> Required
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_device"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Device</FormLabel>
            <FormControl>
              <Input
                placeholder="Device name (optional)"
                {...field}
              />
            </FormControl>
            <FormDescription>
              Leave blank to send to all devices
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_title"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Message Title</FormLabel>
            <FormControl>
              <Input
                placeholder="Custom message title (optional)"
                {...field}
              />
            </FormControl>
            <FormDescription>
              Leave blank to use default title
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_priority"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Priority</FormLabel>
            <Select
              onValueChange={(value) => field.onChange(parseInt(value))}
              value={field.value?.toString()}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder="Select priority" />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {priorityOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value.toString()}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormDescription>
              Message priority level
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_sounds"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Notification Sound - Down</FormLabel>
            <Select
              onValueChange={field.onChange}
              value={field.value}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder="Select sound" />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {soundOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormDescription>
              Sound for when monitor goes down
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_sounds_up"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Notification Sound - Up</FormLabel>
            <Select
              onValueChange={field.onChange}
              value={field.value}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder="Select sound" />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {soundOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormDescription>
              Sound for when monitor comes back up
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_ttl"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Message TTL</FormLabel>
            <FormControl>
              <Input
                type="number"
                min="0"
                step="1"
                placeholder="0"
                {...field}
                onChange={(e) => field.onChange(parseInt(e.target.value) || 0)}
              />
            </FormControl>
            <FormDescription>
              Time-to-live in seconds (0 = no expiration)
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <div className="mt-4 p-4 bg-gray-50 rounded-lg">
        <p className="text-sm text-gray-600">
          <span className="text-red-500">*</span> Required fields
        </p>
        <p className="text-sm text-gray-600 mt-2">
          More info on:{" "}
          <a
            href="https://pushover.net/api"
            target="_blank"
            rel="noopener noreferrer"
            className="text-blue-600 underline"
          >
            https://pushover.net/api
          </a>
        </p>
        <p className="text-sm text-gray-600 mt-2">
          You can create an application and get your API token at{" "}
          <a
            href="https://pushover.net/apps/build"
            target="_blank"
            rel="noopener noreferrer"
            className="text-blue-600 underline"
          >
            https://pushover.net/apps/build
          </a>
        </p>
        <p className="text-sm text-gray-600 mt-2">
          Your user key can be found at{" "}
          <a
            href="https://pushover.net/"
            target="_blank"
            rel="noopener noreferrer"
            className="text-blue-600 underline"
          >
            https://pushover.net/
          </a>
        </p>
      </div>
    </>
  );
}
