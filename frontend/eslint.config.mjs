import { createConfig } from '@agent-teams/eslint-native-syntax';
import policy from './eslint-policy.json' with { type: 'json' };

export default createConfig(policy);
