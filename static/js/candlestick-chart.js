// Candlestick chart using D3.js
// No npm, pure ESM import from CDN

export class CandlestickChart {
  constructor(containerId, options = {}) {
    this.containerId = containerId;
    this.width = options.width || 800;
    this.height = options.height || 400;
    this.margin = options.margin || { top: 20, right: 60, bottom: 30, left: 60 };
    this.svg = null;
    this.data = [];
  }

  async init() {
    // Import D3 dynamically
    this.d3 = await import('https://cdn.jsdelivr.net/npm/d3@7/+esm');
  }

  async render(data) {
    if (!this.d3) {
      await this.init();
    }

    this.data = data;
    const container = document.getElementById(this.containerId);
    if (!container) {
      console.error(`Container ${this.containerId} not found`);
      return;
    }

    // Clear existing content
    container.innerHTML = '';

    // Set up dimensions
    const width = this.width - this.margin.left - this.margin.right;
    const height = this.height - this.margin.top - this.margin.bottom;

    // Create SVG
    const svg = this.d3.select(`#${this.containerId}`)
      .append('svg')
      .attr('width', this.width)
      .attr('height', this.height)
      .append('g')
      .attr('transform', `translate(${this.margin.left},${this.margin.top})`);

    this.svg = svg;

    // Parse dates
    const parseTime = this.d3.isoParse;
    data.forEach(d => {
      d.date = parseTime(d.timestamp);
      d.open = +d.open;
      d.high = +d.high;
      d.low = +d.low;
      d.close = +d.close;
    });

    // Set scales
    const x = this.d3.scaleBand()
      .domain(data.map(d => d.date))
      .range([0, width])
      .padding(0.2);

    const y = this.d3.scaleLinear()
      .domain([
        this.d3.min(data, d => d.low) * 0.99,
        this.d3.max(data, d => d.high) * 1.01
      ])
      .range([height, 0]);

    // Add X axis
    const xAxis = svg.append('g')
      .attr('transform', `translate(0,${height})`)
      .call(this.d3.axisBottom(x)
        .tickValues(x.domain().filter((d, i) => {
          // Show fewer ticks based on data length
          const step = Math.ceil(data.length / 10);
          return i % step === 0;
        }))
        .tickFormat(this.d3.timeFormat('%m/%d')))
      .style('color', '#9ca3af');

    // Add Y axis
    const yAxis = svg.append('g')
      .call(this.d3.axisRight(y)
        .ticks(8)
        .tickFormat(d => `$${d.toFixed(2)}`))
      .attr('transform', `translate(${width},0)`)
      .style('color', '#9ca3af');

    // Remove domain lines
    xAxis.select('.domain').remove();
    yAxis.select('.domain').remove();

    // Style tick lines
    svg.selectAll('.tick line')
      .style('stroke', 'rgba(255, 255, 255, 0.05)');

    // Add grid lines
    svg.append('g')
      .attr('class', 'grid')
      .call(this.d3.axisLeft(y)
        .ticks(8)
        .tickSize(-width)
        .tickFormat(''))
      .style('stroke', 'rgba(255, 255, 255, 0.05)')
      .select('.domain').remove();

    // Create candlesticks
    const candleWidth = x.bandwidth();

    // Draw wicks (high-low lines)
    svg.selectAll('.wick')
      .data(data)
      .enter()
      .append('line')
      .attr('class', 'wick')
      .attr('x1', d => x(d.date) + candleWidth / 2)
      .attr('x2', d => x(d.date) + candleWidth / 2)
      .attr('y1', d => y(d.high))
      .attr('y2', d => y(d.low))
      .attr('stroke', d => d.close >= d.open ? '#10b981' : '#ef4444')
      .attr('stroke-width', 1);

    // Draw candle bodies (open-close rectangles)
    svg.selectAll('.candle')
      .data(data)
      .enter()
      .append('rect')
      .attr('class', 'candle')
      .attr('x', d => x(d.date))
      .attr('y', d => y(Math.max(d.open, d.close)))
      .attr('width', candleWidth)
      .attr('height', d => {
        const h = Math.abs(y(d.open) - y(d.close));
        return h === 0 ? 1 : h; // Minimum 1px for doji candles
      })
      .attr('fill', d => d.close >= d.open ? '#10b981' : '#ef4444')
      .attr('stroke', d => d.close >= d.open ? '#10b981' : '#ef4444')
      .attr('stroke-width', 1);

    // Add tooltip
    const tooltip = this.d3.select('body')
      .append('div')
      .attr('class', 'candlestick-tooltip')
      .style('position', 'absolute')
      .style('visibility', 'hidden')
      .style('background-color', 'rgba(17, 24, 39, 0.95)')
      .style('color', '#e5e5e5')
      .style('border', '1px solid #374151')
      .style('border-radius', '6px')
      .style('padding', '12px')
      .style('font-size', '13px')
      .style('pointer-events', 'none')
      .style('z-index', '1000');

    // Add hover interactions
    svg.selectAll('.candle')
      .on('mouseover', (event, d) => {
        const change = d.close - d.open;
        const changePercent = (change / d.open) * 100;
        const color = change >= 0 ? '#10b981' : '#ef4444';

        tooltip.html(`
          <div style="font-weight: 600; margin-bottom: 6px;">
            ${this.d3.timeFormat('%b %d, %Y')(d.date)}
          </div>
          <div style="display: grid; grid-template-columns: auto auto; gap: 8px 12px;">
            <span style="color: #9ca3af;">Open:</span>
            <span>$${d.open.toFixed(2)}</span>
            <span style="color: #9ca3af;">High:</span>
            <span>$${d.high.toFixed(2)}</span>
            <span style="color: #9ca3af;">Low:</span>
            <span>$${d.low.toFixed(2)}</span>
            <span style="color: #9ca3af;">Close:</span>
            <span>$${d.close.toFixed(2)}</span>
            <span style="color: #9ca3af;">Change:</span>
            <span style="color: ${color};">${change >= 0 ? '+' : ''}$${change.toFixed(2)} (${changePercent >= 0 ? '+' : ''}${changePercent.toFixed(2)}%)</span>
          </div>
        `)
          .style('visibility', 'visible');

        this.d3.select(event.currentTarget)
          .style('opacity', 0.8);
      })
      .on('mousemove', (event) => {
        tooltip
          .style('top', (event.pageY - 10) + 'px')
          .style('left', (event.pageX + 10) + 'px');
      })
      .on('mouseout', (event) => {
        tooltip.style('visibility', 'hidden');
        this.d3.select(event.currentTarget)
          .style('opacity', 1);
      });
  }

  destroy() {
    if (this.svg) {
      this.svg.remove();
    }
    // Remove any tooltips
    this.d3?.selectAll('.candlestick-tooltip').remove();
  }
}
