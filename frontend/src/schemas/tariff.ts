import { z } from 'zod';

const TariffProfileItemSchema = z.object({
  id: z.number(),
  name: z.string(),
  position: z.number(),
});

const ResolvedFieldsSchema = z.object({
  traffic: z.number().optional(),
  expiryDays: z.number().optional(),
  limitIp: z.number().optional(),
  inboundIds: z.array(z.number()).optional(),
});

export const TariffSchema = z.object({
  id: z.number(),
  name: z.string(),
  trafficStrategy: z.string(),
  inboundStrategy: z.string(),
  enable: z.boolean(),
  profiles: z.array(TariffProfileItemSchema).optional(),
  resolved: ResolvedFieldsSchema.optional().nullable(),
  groupCount: z.number(),
  clientCount: z.number().optional(),
  createdAt: z.number().optional(),
  updatedAt: z.number().optional(),
});

export type Tariff = z.infer<typeof TariffSchema>;

export const TariffFormSchema = z.object({
  name: z.string().trim().min(1, 'pages.tariffs.name'),
  trafficStrategy: z.string(),
  inboundStrategy: z.string(),
});

export type TariffFormValues = z.infer<typeof TariffFormSchema>;
