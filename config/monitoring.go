package config

type CMonitoring struct {
	Rules       []string
	Removefiles []string

	Channel chan struct{}
}

func NewCMonitoring() *CMonitoring {
	return &CMonitoring{Channel: make(chan struct{})}
}

func (c *CMonitoring) JudgeChange(files []string) {
	c.Removefiles = []string{}

	for _, cfile := range c.Rules {
		in := false
		for _, file := range files {
			if file == cfile {
				in = true
				break
			}
		}
		if in == false {
			c.Removefiles = append(c.Removefiles, cfile)
		}
	}
	c.Rules = files
	c.Channel <- struct{}{}
	return
}
