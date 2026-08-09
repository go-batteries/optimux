local _M = {}

function _M.build_tags()
	return {
		env = os.getenv("ENVIRONMENT"),
		service = os.getenv("DD_SERVICE"),
	}
end

return _M
