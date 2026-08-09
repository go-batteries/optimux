local math = require("math")
local os = require("os")
local socket = require("socket")
local nsocket = ngx.socket

math.randomseed(os.time())

local function send_to_socket(self, string, tags)
	if tags and #tags > 0 then
		string = string .. "|#" .. table.concat(tags, ",")
	end
	return self.udp:send(string)
end

local function make_statsd_message(self, stat, delta, kind, sample_rate)
	local prefix = self.namespace and (self.namespace .. ".") or ""
	stat = stat:gsub("[:|@]", "_")
	local rate = (sample_rate and sample_rate ~= 1) and "|@" .. sample_rate or ""
	return prefix .. stat .. ":" .. delta .. "|" .. kind .. rate
end

local function send(self, stat, delta, kind, sample_rate, neg, tags)
	local packet_size = self.packet_size
	sample_rate = sample_rate or 1

	if not (sample_rate == 1 or math.random() <= sample_rate) then
		return
	end

	local stat_type = type(stat)
	local msg

	if stat_type == "table" then
		local t, size = {}, 0
		for s, v in pairs(stat) do
			if kind == "c" then
				if type(s) == "number" then
					s, v = v, 1
				end
				v = neg and -v or v
			end
			msg = make_statsd_message(self, s, v, kind, sample_rate)
			size = size + #msg
			if t[1] and size > packet_size then
				local msg_batch = table.concat(t, "\n")
				local ok, err = send_to_socket(self, msg_batch, tags)
				if not ok then
					return nil, err
				end
				t, size = {}, 0
			end
			t[#t + 1] = msg
		end
		msg = table.concat(t, "\n")
	else
		msg = make_statsd_message(self, stat, delta, kind, sample_rate)
	end

	return send_to_socket(self, msg, tags)
end

-- metric types
local function gauge(self, stat, value, sample_rate, tags)
	return send(self, stat, value, "g", sample_rate, false, tags)
end

local function counter_(self, stat, value, sample_rate, neg, tags)
	return send(self, stat, value, "c", sample_rate, neg, tags)
end

local function counter(self, stat, value, sample_rate, tags)
	return counter_(self, stat, value, sample_rate, false, tags)
end

local function increment(self, stat, value, sample_rate, tags)
	return counter_(self, stat, value or 1, sample_rate, false, tags)
end

local function decrement(self, stat, value, sample_rate, tags)
	value = value or 1
	if type(stat) == "string" then
		value = -value
	end
	return counter_(self, stat, value, sample_rate, true, tags)
end

local function timer(self, stat, ms, tags)
	return send(self, stat, ms, "ms", nil, false, tags)
end

local function histogram(self, stat, value, tags)
	return send(self, stat, value, "h", nil, false, tags)
end

local function meter(self, stat, value, tags)
	return send(self, stat, value, "m", nil, false, tags)
end

local function set(self, stat, value, tags)
	return send(self, stat, value, "s", nil, false, tags)
end

-- main constructor
return function(options)
	options = options or {}

	local host = options.host or "127.0.0.1"
	local port = options.port or 8125
	local namespace = options.namespace or nil
	local packet_size = options.packet_size or 508
	local nonblock = options.nonblock or false

	local udp = socket.udp()
	udp:settimeout(1)

	if nonblock then
		local ok, err = udp:setpeername(host, port)
		if not ok then
			return nil, err
		end
	else
		local ok = udp:setpeername(host, port)
		if not ok then
			return nil, error("upd: failed to connect")
		end
	end

	return {
		namespace = namespace,
		udp = udp,
		packet_size = packet_size,
		gauge = gauge,
		counter = counter,
		increment = increment,
		decrement = decrement,
		timer = timer,
		histogram = histogram,
		meter = meter,
		set = set,
	},
		nil
end
