import { MultiSelect } from "@/components/multi-select";
import {
  FormControl,
  FormField,
  FormLabel,
  FormMessage,
  FormDescription,
} from "@/components/ui/form";
import { FormItem } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { TypographyH4 } from "@/components/ui/typography";
import { useFormContext } from "react-hook-form";
import { z } from "zod";

const acceptedStatusCodesOptions = [
  { value: "1XX", label: "1XX" },
  { value: "2XX", label: "2XX" },
  { value: "3XX", label: "3XX" },
  { value: "4XX", label: "4XX" },
  { value: "5XX", label: "5XX" },
];

export const advancedSchema = z.object({
  accepted_statuscodes: z.array(z.string()),
  max_redirects: z.coerce.number().min(0).max(30),
  ignore_tls_errors: z.boolean(),
});

export type AdvancedForm = z.infer<typeof advancedSchema>;

export const advancedDefaultValues: AdvancedForm = {
  accepted_statuscodes: ["2XX"],
  max_redirects: 10,
  ignore_tls_errors: false,
}

const Advanced = () => {
  const form = useFormContext();

  return (
    <>
      <TypographyH4>Advanced</TypographyH4>

      <FormField
        control={form.control}
        name="accepted_statuscodes"
        render={({ field }) => {
          return <FormItem>
          <FormLabel>Accepted Status Codes</FormLabel>
          <FormControl>
            <MultiSelect
              options={acceptedStatusCodesOptions}
              onValueChange={(val) => {
                field.onChange(val)
              }}
              value={field.value || []}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
        }}
      />

      <FormField
        control={form.control}
        name="max_redirects"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Maximum Redirects</FormLabel>
            <FormControl>
              <Input placeholder="10" {...field} type="number" />
            </FormControl>
            <FormDescription>
              Maximum number of redirects to follow (0-30). Set to 0 to disable redirects.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="ignore_tls_errors"
        render={({ field }) => (
          <FormItem className="flex flex-row items-start space-x-3 space-y-0">
            <FormControl>
              <Checkbox
                checked={field.value}
                onCheckedChange={field.onChange}
              />
            </FormControl>
            <div className="space-y-1 leading-none">
              <FormLabel>
                Ignore TLS/SSL errors for HTTPS websites
              </FormLabel>
              <FormDescription>
                Skip TLS certificate validation. Use with caution - this makes connections less secure.
              </FormDescription>
            </div>
          </FormItem>
        )}
      />
    </>
  );
};

export default Advanced;
