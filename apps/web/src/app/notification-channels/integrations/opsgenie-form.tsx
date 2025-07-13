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
import { useFormContext } from "react-hook-form";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export const schema = z.object({
  type: z.literal("opsgenie"),
  region: z.enum(["us", "eu"], { message: "Region is required" }),
  api_key: z.string().min(1, { message: "API key is required" }),
  priority: z.number().min(1).max(5).optional(),
});

export type OpsgenieFormValues = z.infer<typeof schema>;

export const defaultValues: OpsgenieFormValues = {
  type: "opsgenie",
  region: "us",
  api_key: "",
  priority: 3,
};

export const displayName = "Opsgenie";

export default function OpsgenieForm() {
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="region"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              Region <span className="text-red-500">*</span>
            </FormLabel>
            <Select onValueChange={field.onChange} value={field.value}>
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder="Select region" />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="us">US (Default)</SelectItem>
                <SelectItem value="eu">EU</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              <span className="text-red-500">*</span> Required
              <br />
              Choose your Opsgenie region. This determines which API endpoint
              will be used.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="api_key"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              API Key <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="Enter your Opsgenie API key"
                type="password"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> Required
              <br />
              <span className="mt-2 block">
                More info about API keys:{" "}
                <a
                  href="https://docs.opsgenie.com/docs/alert-api"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline text-blue-600"
                >
                  https://docs.opsgenie.com/docs/alert-api
                </a>
              </span>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

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
                <SelectItem value="1">P1 - Critical</SelectItem>
                <SelectItem value="2">P2 - High</SelectItem>
                <SelectItem value="3">P3 - Moderate (Default)</SelectItem>
                <SelectItem value="4">P4 - Low</SelectItem>
                <SelectItem value="5">P5 - Informational</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              Set the priority level for alerts. Default is P3 (Moderate).
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
