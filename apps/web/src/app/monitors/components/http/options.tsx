import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import {
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { TypographyH4 } from "@/components/ui/typography";
import { isJson, isValidForm, isValidXml } from "@/lib/utils";
import { Select } from "@radix-ui/react-select";
import { useFormContext } from "react-hook-form";
import { z } from "zod";

// http methods
const httpMethods = [
  { value: "GET", label: "GET" },
  { value: "POST", label: "POST" },
  { value: "PUT", label: "PUT" },
  { value: "DELETE", label: "DELETE" },
  { value: "HEAD", label: "HEAD" },
  { value: "OPTIONS", label: "OPTIONS" },
];
const encoding = [
  { value: "json", label: "JSON" },
  { value: "form", label: "Form" },
  { value: "text", label: "Text" },
  { value: "xml", label: "XML" },
];

const headersPlaceholder = `Example:
{
  "HeaderName": "HeaderValue"
}
`;

const bodyPlaceholder = `Example:
{
  "key": "value"
}
`;

const base = z.object({
  method: z.enum(["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"]),
  headers: z.string().refine(isJson, { message: "Invalid JSON" }),
});

const jsonSchema = base.extend({
  encoding: z.literal("json"),
  body: z.string().refine(isJson, { message: "Invalid JSON" }),
});

const formSchema = base.extend({
  encoding: z.literal("form"),
  body: z.string().refine(isValidForm, { message: "Invalid form data" }),
});

const textSchema = base.extend({
  encoding: z.literal("text"),
  body: z.string(),
});

const xmlSchema = base.extend({
  encoding: z.literal("xml"),
  body: z.string().refine(isValidXml, { message: "Invalid XML" }),
});

export const httpOptionsSchema = z.discriminatedUnion("encoding", [
  jsonSchema,
  formSchema,
  textSchema,
  xmlSchema,
]);

export type HttpOptionsForm = z.infer<typeof httpOptionsSchema>;

export const httpOptionsDefaultValues: HttpOptionsForm = {
  method: "GET",
  encoding: "json",
  body: "",
  headers: '{ "Content-Type": "application/json" }',
};

const HttpOptions = () => {
  const form = useFormContext();
  const watchedEncoding = form.watch("httpOptions.encoding");

  // Dynamic placeholders based on encoding
  const getBodyPlaceholder = (encoding: string) => {
    switch (encoding) {
      case "json":
        return `Example JSON:
{
  "key": "value",
  "number": 123
}`;
      case "xml":
        return `Example XML:
<?xml version="1.0" encoding="UTF-8"?>
<root>
  <key>value</key>
  <number>123</number>
</root>`;
      case "form":
        return `Example Form Data:
key1=value1&key2=value2
  `;
      //   Or JSON format:
      // {
      //   "key1": "value1",
      //   "key2": "value2"
      // }
      case "text":
        return `Example Text:
Any plain text content here...`;
      default:
        return bodyPlaceholder;
    }
  };

  return (
    <>
      <TypographyH4>HTTP Options</TypographyH4>
      <FormField
        control={form.control}
        name="httpOptions.method"
        render={({ field }) => {
          return (
            <FormItem>
              <FormLabel>Method</FormLabel>
              <Select
                onValueChange={(e) => {
                  if (!e) {
                    return;
                  }
                  field.onChange(e);
                }}
                value={field.value}
              >
                <FormControl>
                  <SelectTrigger>
                    <SelectValue placeholder="Select monitor type" />
                  </SelectTrigger>
                </FormControl>

                <SelectContent>
                  {httpMethods.map((method) => (
                    <SelectItem key={method.value} value={method.value}>
                      {method.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          );
        }}
      />

      <FormField
        control={form.control}
        name="httpOptions.encoding"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Body encoding</FormLabel>
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
                  <SelectValue placeholder="Select body encoding" />
                </SelectTrigger>
              </FormControl>

              <SelectContent>
                {encoding.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="httpOptions.body"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Body</FormLabel>
            <Textarea
              {...field}
              placeholder={getBodyPlaceholder(watchedEncoding || "json")}
            />
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="httpOptions.headers"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Headers</FormLabel>
            <Textarea {...field} placeholder={headersPlaceholder} />
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

export default HttpOptions;
