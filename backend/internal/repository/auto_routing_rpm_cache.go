package repository

import (
	"context"
	"fmt"
)

const autoRoutingRPMAdmissionScript = `
local user_seen = redis.call('EXISTS', KEYS[3]) == 1
local group_seen = redis.call('EXISTS', KEYS[4]) == 1
local user_limit = tonumber(ARGV[1])
local group_limit = tonumber(ARGV[2])
if not user_seen and user_limit > 0 and tonumber(redis.call('GET', KEYS[1]) or '0') >= user_limit then return 2 end
if not group_seen and group_limit > 0 and tonumber(redis.call('GET', KEYS[2]) or '0') >= group_limit then return 1 end
if not user_seen then
  if user_limit > 0 then redis.call('INCR', KEYS[1]); redis.call('EXPIRE', KEYS[1], 120) end
  redis.call('SET', KEYS[3], '1', 'EX', 86400)
end
if not group_seen then
  if group_limit > 0 then redis.call('INCR', KEYS[2]); redis.call('EXPIRE', KEYS[2], 120) end
  redis.call('SET', KEYS[4], '1', 'EX', 86400)
end
return 0
`

func (c *userRPMCacheImpl) AdmitAutoRoutingRPM(ctx context.Context, userID, groupID, executionID, attemptNo int64, userLimit, groupLimit int) (int, error) {
	minute, err := c.minuteTS(ctx)
	if err != nil {
		return 0, err
	}
	keys := []string{
		fmt.Sprintf("%s%d:%d", userRPMKeyPrefix, userID, minute),
		fmt.Sprintf("%s%d:%d:%d", userGroupRPMKeyPrefix, userID, groupID, minute),
		fmt.Sprintf("rpm:auto:%d:%d", userID, executionID),
		fmt.Sprintf("rpm:auto-group:%d:%d:%d", userID, executionID, attemptNo),
	}
	return c.rdb.Eval(ctx, autoRoutingRPMAdmissionScript, keys, userLimit, groupLimit).Int()
}
