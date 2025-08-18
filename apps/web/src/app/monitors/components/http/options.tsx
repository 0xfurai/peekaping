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
import { useLocalizedTranslation } from "@/hooks/useTranslation";

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
  const { t } = useLocalizedTranslation();
  const form = useFormContext();
  const watchedEncoding = form.watch("httpOptions.encoding");

  // Dynamic placeholders based on encoding
  const getBodyPlaceholder = (encoding: string) => {
    switch (encoding) {
      case "json":
        return `${t("monitors.form.http.options.json_example")}:
{
  "key": "value",
  "number": 123
}`;
      case "xml":
        return `${t("monitors.form.http.options.xml_example")}:
<?xml version="1.0" encoding="UTF-8"?>
<root>
  <key>value</key>
  <number>123</number>
</root>`;
      case "form":
        return `${t("monitors.form.http.options.form_example")}:
key1=value1&key2=value2
  `;
      //   Or JSON format:
      // {
      //   "key1": "value1",
      //   "key2": "value2"
      // }
      case "text":
        return t("monitors.form.http.options.text_example");
      default:
        return bodyPlaceholder;
    }
  };

  return (
    <>
      <TypographyH4>{t("monitors.form.http.options.title")}</TypographyH4>
      <FormField
        control={form.control}
        name="httpOptions.method"
        render={({ field }) => {
          return (
            <FormItem>
              <FormLabel>{t("monitors.form.http.options.method")}</FormLabel>
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
                    <SelectValue placeholder={t("monitors.form.http.options.select_method")} />
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
            <FormLabel>{t("monitors.form.http.options.body_encoding")}</FormLabel>
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
                  <SelectValue placeholder={t("monitors.form.http.options.select_encoding")} />
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
            <FormLabel>{t("monitors.form.http.options.body")}</FormLabel>
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
            <FormLabel>{t("monitors.form.http.options.headers")}</FormLabel>
            <Textarea {...field} placeholder={headersPlaceholder} />
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

export default HttpOptions;
