import { z } from "zod";
import { withSavedSecret } from "../../secrets";

  
  
export const weComSchema = z.object({  
  type: z.literal("wecom"),  
  webhook_url: withSavedSecret(z.string().url({ message: "Valid webhook URL is required" })),  
  custom_message: z.string().optional(),  
});

  
  
export type WeComFormValues = z.infer<typeof weComSchema>;

  
  
export const weComDefaultValues: WeComFormValues = {  
  type: "wecom",  
  webhook_url: "",  
  custom_message: "{{ msg }}",  
};
