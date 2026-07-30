export type ActivationStage = 'create_key' | 'send_request' | 'activated';

export function activationState({
  activeKeys,
  totalRequests,
}: {
  activeKeys: number;
  totalRequests: number;
}): { stage: ActivationStage; completedSteps: number } {
  if (activeKeys <= 0) return { stage: 'create_key', completedSteps: 1 };
  if (totalRequests <= 0) return { stage: 'send_request', completedSteps: 2 };
  return { stage: 'activated', completedSteps: 3 };
}

